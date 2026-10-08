package httpapi

import (
	"mime"
	"net/http"
	"strconv"

	"phantom-mail/internal/message"
)

// getAttachment always serves attachments as downloads. nosniff plus a
// sandbox policy mean a hostile attachment (an HTML file, say) can neither be
// rendered by content sniffing nor run if the link is opened directly.
func (s *Server) getAttachment(w http.ResponseWriter, r *http.Request) {
	box, ok := mailboxParam(w, r)
	if !ok {
		return
	}
	_, raw, found := s.fetch(w, box, r.PathValue("id"))
	if !found {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("index"))
	atts := message.Parse(raw).Attachments
	if err != nil || idx < 0 || idx >= len(atts) {
		writeError(w, http.StatusNotFound, "not_found", "no such attachment")
		return
	}
	a := atts[idx]

	ctype := "application/octet-stream"
	if mt, params, err := mime.ParseMediaType(a.ContentType); err == nil {
		ctype = mime.FormatMediaType(mt, params)
		if ctype == "" {
			ctype = "application/octet-stream"
		}
	}
	disp := mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename})
	if disp == "" {
		disp = "attachment"
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", disp)
	h.Set("Content-Length", strconv.Itoa(len(a.Data)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, no-store")
	_, _ = w.Write(a.Data)
}
