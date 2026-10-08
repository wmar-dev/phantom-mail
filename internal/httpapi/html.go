package httpapi

import (
	"html"
	"net/http"

	"phantom-mail/internal/message"
)

// htmlCSP lets nothing run or load: no scripts, no remote images, fonts,
// styles, frames or forms. Inline styles and data: images are the only
// allowances so ordinary email layout still renders. The sandbox directive
// adds a second layer even if the page is opened outside the UI's iframe.
const htmlCSP = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'self'; sandbox"

func (s *Server) getHTML(w http.ResponseWriter, r *http.Request) {
	box, ok := mailboxParam(w, r)
	if !ok {
		return
	}
	_, raw, found := s.fetch(w, box, r.PathValue("id"))
	if !found {
		return
	}
	p := message.Parse(raw)
	body := p.HTML
	if body == "" {
		body = `<pre style="white-space:pre-wrap;font:14px/1.5 ui-monospace,monospace">` + html.EscapeString(p.Text) + `</pre>`
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", htmlCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "private, no-store")
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><meta name="referrer" content="no-referrer">` + body))
}
