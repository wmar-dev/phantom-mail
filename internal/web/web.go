// Package web serves the embedded single-page web interface. Assets are
// compressed once at startup and served with ETags; there is no build step
// and no third-party request.
package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed static/*.html static/*.css static/*.js
var files embed.FS

type asset struct {
	body  []byte
	gz    []byte
	ctype string
	etag  string
}

// csp restricts the interface to its own origin. The email body lives in a
// sandboxed same-origin iframe (frame-src 'self') whose own, stricter policy
// is set by the API.
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; frame-src 'self'; object-src 'none'; base-uri 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
}

// Handler returns the handler for the web interface.
func Handler() http.Handler {
	assets := map[string]*asset{}
	entries, err := files.ReadDir("static")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		body, err := files.ReadFile("static/" + e.Name())
		if err != nil {
			panic(err)
		}
		sum := sha256.Sum256(body)
		a := &asset{body: body, ctype: contentTypes[path.Ext(e.Name())], etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(body)
		_ = zw.Close()
		if buf.Len() < len(body) {
			a.gz = buf.Bytes()
		}
		assets["/"+e.Name()] = a
	}
	assets["/"] = assets["/index.html"]

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a := assets[r.URL.Path]
		if a == nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", a.ctype)
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-cache") // always revalidate; ETags make that cheap
		h.Set("Vary", "Accept-Encoding")
		h.Set("ETag", a.etag)
		if match := r.Header.Get("If-None-Match"); match == a.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body := a.body
		if a.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.Set("Content-Encoding", "gzip")
			body = a.gz
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
}
