package message

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxHeaderBytes = 256 << 10
	maxDepth       = 10
	// maxParts bounds the work and memory a hostile message full of tiny MIME
	// parts can cause each time it is parsed; parts beyond it are ignored.
	maxParts = 1000
)

// Headers is the subset of header information the service indexes.
type Headers struct {
	From       string
	Subject    string
	To         []string // original recipients (X-Original-To, else To header)
	ReturnPath string
}

type header map[string][]string

func (h header) get(key string) string {
	if v := h[textproto.CanonicalMIMEHeaderKey(key)]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// splitHeadBody splits at the first blank line. It accepts CRLF and LF.
func splitHeadBody(raw []byte) (head, body []byte) {
	pos := 0
	for pos < len(raw) {
		end := bytes.IndexByte(raw[pos:], '\n')
		var line []byte
		next := len(raw)
		if end >= 0 {
			line = raw[pos : pos+end]
			next = pos + end + 1
		} else {
			line = raw[pos:]
		}
		if len(bytes.TrimRight(line, "\r")) == 0 {
			return raw[:pos], raw[next:]
		}
		pos = next
	}
	return raw, nil
}

// parseHeaderBlock is a forgiving header parser: lines that are not headers
// are skipped instead of failing the message.
func parseHeaderBlock(head []byte) header {
	h := header{}
	var key string
	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && key != "" {
			vals := h[key]
			vals[len(vals)-1] += " " + strings.TrimSpace(line)
			continue
		}
		i := strings.IndexByte(line, ':')
		if i <= 0 || strings.ContainsAny(line[:i], " \t") {
			key = ""
			continue
		}
		key = textproto.CanonicalMIMEHeaderKey(line[:i])
		h[key] = append(h[key], strings.TrimSpace(line[i+1:]))
	}
	return h
}

var wordDecoder = &mime.WordDecoder{CharsetReader: func(label string, r io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(decodeCharset(label, b)), nil
}}

func decodeWords(s string) string {
	if d, err := wordDecoder.DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// ParseHeaders extracts the indexed header fields from a raw message.
func ParseHeaders(raw []byte) Headers {
	if len(raw) > maxHeaderBytes {
		raw = raw[:maxHeaderBytes]
	}
	head, _ := splitHeadBody(raw)
	h := parseHeaderBlock(head)

	out := Headers{
		Subject:    strings.Join(strings.Fields(decodeWords(h.get("Subject"))), " "),
		From:       decodeWords(h.get("From")),
		ReturnPath: strings.Trim(strings.TrimSpace(h.get("Return-Path")), "<>"),
	}
	if out.From == "" {
		out.From = out.ReturnPath
	}
	for _, v := range h["X-Original-To"] {
		if v = strings.TrimSpace(v); v != "" {
			out.To = append(out.To, v)
		}
	}
	if len(out.To) == 0 {
		out.To = addressList(h.get("To"))
	}
	return out
}

func addressList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	if list, err := mail.ParseAddressList(v); err == nil {
		out := make([]string, 0, len(list))
		for _, a := range list {
			out = append(out, a.Address)
		}
		return out
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if i := strings.IndexByte(part, '<'); i >= 0 {
			if j := strings.IndexByte(part[i:], '>'); j > 0 {
				part = part[i+1 : i+j]
			}
		}
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Parse decodes the text and HTML bodies and the attachments of a raw
// message. It never fails: damaged input yields as much content as possible.
func Parse(raw []byte) Parsed {
	head, body := splitHeadBody(raw)
	c := &collector{}
	c.walk(parseHeaderBlock(head), body, 0)
	return Parsed{
		Text:        strings.TrimRight(c.text.String(), " \t\r\n"),
		HTML:        strings.TrimRight(c.html.String(), " \t\r\n"),
		Attachments: c.atts,
	}
}

type collector struct {
	text, html strings.Builder
	atts       []Attachment
	parts      int
}

func (c *collector) walk(h header, body []byte, depth int) {
	mediaType, params, err := mime.ParseMediaType(h.get("Content-Type"))
	if err != nil && mediaType == "" {
		mediaType, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mediaType, "multipart/") && depth < maxDepth && params["boundary"] != "" {
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			if c.parts >= maxParts {
				return
			}
			p, err := mr.NextRawPart()
			if err != nil {
				return
			}
			data, _ := io.ReadAll(p)
			c.walk(header(p.Header), data, depth+1)
		}
	}
	c.leaf(h, body, mediaType, params)
}

func (c *collector) leaf(h header, body []byte, mediaType string, params map[string]string) {
	c.parts++
	disp, dparams, _ := mime.ParseMediaType(h.get("Content-Disposition"))
	name := dparams["filename"]
	if name == "" {
		name = params["name"]
	}
	name = decodeWords(name)

	data := decodeTransfer(h.get("Content-Transfer-Encoding"), body)
	isBody := (mediaType == "text/plain" || mediaType == "text/html") && disp != "attachment" && name == ""
	if !isBody {
		idx := len(c.atts)
		ct := mediaType
		if ct == "" || strings.HasPrefix(ct, "multipart/") {
			ct = "application/octet-stream"
		}
		c.atts = append(c.atts, Attachment{
			AttachmentInfo: AttachmentInfo{Index: idx, Filename: sanitizeFilename(name, idx), ContentType: ct, Size: len(data)},
			Data:           data,
		})
		return
	}
	text := decodeCharset(params["charset"], data)
	dst := &c.text
	if mediaType == "text/html" {
		dst = &c.html
	}
	if dst.Len() > 0 {
		dst.WriteString("\n")
	}
	dst.WriteString(text)
}

func sanitizeFilename(name string, idx int) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if len(name) > 255 {
		name = name[:255]
		for !utf8.ValidString(name) {
			name = name[:len(name)-1]
		}
	}
	if name == "" || name == "." || name == ".." {
		return "attachment-" + strconv.Itoa(idx)
	}
	return name
}

func decodeTransfer(enc string, body []byte) []byte {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		clean := make([]byte, 0, len(body))
		for _, b := range body {
			if b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '+' || b == '/' {
				clean = append(clean, b)
			}
		}
		out := make([]byte, base64.RawStdEncoding.DecodedLen(len(clean)))
		n, err := base64.RawStdEncoding.Decode(out, clean)
		if err != nil {
			return body
		}
		return out[:n]
	case "quoted-printable":
		return decodeQP(body)
	}
	return body
}

// decodeQP is a lenient quoted-printable decoder: malformed escapes are kept
// literally instead of aborting.
func decodeQP(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		b := in[i]
		if b != '=' {
			out = append(out, b)
			continue
		}
		if i+2 < len(in) {
			if hi, ok1 := unhex(in[i+1]); ok1 {
				if lo, ok2 := unhex(in[i+2]); ok2 {
					out = append(out, hi<<4|lo)
					i += 2
					continue
				}
			}
		}
		// soft line break: "=" at end of line (optionally before spaces)
		j := i + 1
		for j < len(in) && (in[j] == ' ' || in[j] == '\t') {
			j++
		}
		if j < len(in) && in[j] == '\r' {
			j++
		}
		if j < len(in) && in[j] == '\n' {
			i = j
			continue
		}
		if j >= len(in) {
			break
		}
		out = append(out, b)
	}
	return out
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

var cp1252High = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// decodeCharset converts b to valid UTF-8. UTF-8, US-ASCII, ISO-8859-1 and
// Windows-1252 are decoded exactly; any other charset falls back to lossy
// UTF-8 with replacement characters so the message is still displayable.
func decodeCharset(label string, b []byte) string {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(label), `"'`)) {
	case "iso-8859-1", "iso8859-1", "iso_8859-1", "latin1", "l1", "cp819", "ibm819":
		rs := make([]rune, len(b))
		for i, c := range b {
			rs[i] = rune(c)
		}
		return string(rs)
	case "windows-1252", "cp1252", "x-cp1252":
		rs := make([]rune, len(b))
		for i, c := range b {
			if c >= 0x80 && c <= 0x9F {
				rs[i] = cp1252High[c-0x80]
			} else {
				rs[i] = rune(c)
			}
		}
		return string(rs)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), "�")
}
