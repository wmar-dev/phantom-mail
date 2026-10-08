package message

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseTextOnly(t *testing.T) {
	p := Parse(fixture(t, "text_only.eml"))
	if !strings.Contains(p.Text, "482913") {
		t.Fatalf("text = %q", p.Text)
	}
	if p.HTML != "" || len(p.Attachments) != 0 {
		t.Fatalf("unexpected html/attachments: %+v", p)
	}
}

func TestParseHTMLOnly(t *testing.T) {
	p := Parse(fixture(t, "html_only.eml"))
	if !strings.Contains(p.HTML, "<b>123456</b>") {
		t.Fatalf("html = %q", p.HTML)
	}
	if p.Text != "" {
		t.Fatalf("text = %q, want empty", p.Text)
	}
}

func TestParseMultipartAlternative(t *testing.T) {
	p := Parse(fixture(t, "multipart_alternative.eml"))
	if !strings.Contains(p.Text, "plain part 654321") {
		t.Fatalf("text = %q", p.Text)
	}
	if !strings.Contains(p.HTML, "html part") {
		t.Fatalf("html = %q", p.HTML)
	}
	if len(p.Attachments) != 0 {
		t.Fatalf("attachments = %d", len(p.Attachments))
	}
}

func TestParseAttachments(t *testing.T) {
	p := Parse(fixture(t, "attachments.eml"))
	if !strings.Contains(p.Text, "see attached") || !strings.Contains(p.HTML, "see attached") {
		t.Fatalf("bodies lost: %q / %q", p.Text, p.HTML)
	}
	if len(p.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(p.Attachments))
	}
	a := p.Attachments[0]
	if a.Index != 0 || a.Filename != "report.txt" || string(a.Data) != "hello attachment" || a.Size != len(a.Data) {
		t.Fatalf("attachment 0 = %+v", a)
	}
	b := p.Attachments[1]
	if b.Index != 1 || strings.ContainsAny(b.Filename, "/\\") || b.Filename == "" {
		t.Fatalf("attachment 1 filename not sanitized: %q", b.Filename)
	}
	if string(b.Data) != "secret" {
		t.Fatalf("attachment 1 data = %q", b.Data)
	}
}

func TestParseNoSubject(t *testing.T) {
	raw := fixture(t, "no_subject.eml")
	h := ParseHeaders(raw)
	if h.Subject != "" {
		t.Fatalf("subject = %q", h.Subject)
	}
	if !strings.Contains(Parse(raw).Text, "body without subject") {
		t.Fatal("body lost")
	}
}

func TestParseQuotedPrintable(t *testing.T) {
	p := Parse(fixture(t, "quoted_printable.eml"))
	if !strings.Contains(p.Text, "Café ✓ softbreak") {
		t.Fatalf("text = %q", p.Text)
	}
}

func TestParseBase64(t *testing.T) {
	p := Parse(fixture(t, "base64.eml"))
	if !strings.Contains(p.Text, "Hello base64 world") {
		t.Fatalf("text = %q", p.Text)
	}
}

func TestParseLatin1(t *testing.T) {
	raw := fixture(t, "latin1.eml")
	p := Parse(raw)
	if !strings.Contains(p.Text, "Café") {
		t.Fatalf("text = %q", p.Text)
	}
	if h := ParseHeaders(raw); h.Subject != "Café menu" {
		t.Fatalf("subject = %q", h.Subject)
	}
}

func TestParseWindows1252(t *testing.T) {
	p := Parse(fixture(t, "windows1252.eml"))
	if !strings.Contains(p.Text, "“quoted” €") {
		t.Fatalf("text = %q", p.Text)
	}
}

func TestParseUnknownCharsetFallsBack(t *testing.T) {
	p := Parse(fixture(t, "unknown_charset.eml"))
	if !strings.HasPrefix(p.Text, "caf") || !strings.Contains(p.Text, " ok") {
		t.Fatalf("text = %q", p.Text)
	}
	if !utf8.ValidString(p.Text) {
		t.Fatalf("text is not valid UTF-8: %q", p.Text)
	}
}

func TestParseMalformedHeaders(t *testing.T) {
	raw := fixture(t, "malformed_headers.eml")
	h := ParseHeaders(raw)
	if h.From != "sender@example.com" || h.Subject != "still works" {
		t.Fatalf("headers = %+v", h)
	}
	if !strings.Contains(Parse(raw).Text, "body survives") {
		t.Fatal("body lost")
	}
}

func TestParseGarbageDoesNotPanic(t *testing.T) {
	inputs := [][]byte{
		nil,
		[]byte(""),
		[]byte("\n\n\n"),
		[]byte("Content-Type: multipart/mixed\n\nno boundary"),
		[]byte("Content-Type: multipart/mixed; boundary=x\n\n--x\nbroken"),
		[]byte("Content-Transfer-Encoding: base64\n\n!!!not base64!!!"),
		[]byte("\xff\xfe\x00binary"),
	}
	for i, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("input %d panicked: %v", i, r)
				}
			}()
			_ = Parse(in)
			_ = ParseHeaders(in)
		}()
	}
}

func TestParseHeadersEnvelope(t *testing.T) {
	raw := []byte("Return-Path: <bounce@example.com>\r\nX-Original-To: alice+shop@localhost\r\nX-Original-To: alice@localhost\r\nFrom: Bob <bob@example.com>\r\nSubject: hi\r\n\r\nbody")
	h := ParseHeaders(raw)
	if h.From != "Bob <bob@example.com>" {
		t.Fatalf("from = %q", h.From)
	}
	if len(h.To) != 2 || h.To[0] != "alice+shop@localhost" {
		t.Fatalf("to = %v", h.To)
	}
	if h.ReturnPath != "bounce@example.com" {
		t.Fatalf("return-path = %q", h.ReturnPath)
	}
}

func TestParseHeadersFallsBackToToHeader(t *testing.T) {
	raw := []byte("From: a@example.com\nTo: x@localhost, Y <y@localhost>\nSubject: s\n\nb")
	h := ParseHeaders(raw)
	if len(h.To) != 2 || h.To[0] != "x@localhost" || h.To[1] != "y@localhost" {
		t.Fatalf("to = %v", h.To)
	}
}

func TestIDsAreMonotonicAndValid(t *testing.T) {
	g := NewIDGen(fakeNow{})
	prev := ""
	for i := 0; i < 1000; i++ {
		id := g.New()
		if !ValidID(id) {
			t.Fatalf("invalid id %q", id)
		}
		if id <= prev {
			t.Fatalf("id %q not greater than %q", id, prev)
		}
		prev = id
	}
}

func TestIDTime(t *testing.T) {
	g := NewIDGen(fakeNow{})
	ts, ok := IDTime(g.New())
	if !ok || !ts.Equal(fakeNow{}.Now().Truncate(1e6)) {
		t.Fatalf("IDTime = %v, %v", ts, ok)
	}
	if _, ok := IDTime("nonsense"); ok {
		t.Fatal("IDTime accepted nonsense")
	}
}

func TestHostileMessageWithThousandsOfPartsIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("Subject: bomb\nContent-Type: multipart/mixed; boundary=x\n\n")
	for i := 0; i < 20000; i++ {
		b.WriteString("--x\nContent-Disposition: attachment; filename=f\n\nd\n")
	}
	b.WriteString("--x--\n")
	p := Parse([]byte(b.String()))
	if len(p.Attachments) > 1000 {
		t.Fatalf("parsed %d attachments; the part limit is not applied", len(p.Attachments))
	}
	if len(p.Attachments) == 0 {
		t.Fatal("expected the first parts to be kept")
	}
}

func TestDeeplyNestedMultipartIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("Subject: nest\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=b%d\n\n--b%d\n", i, i)
	}
	b.WriteString("Content-Type: text/plain\n\ndeep\n")
	_ = Parse([]byte(b.String())) // must return, not recurse forever
}
