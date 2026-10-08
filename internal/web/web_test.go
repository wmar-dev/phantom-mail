package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, path string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	return rec
}

func TestIndexAndAssetsAreServed(t *testing.T) {
	cases := map[string]string{
		"/":           "text/html",
		"/app.js":     "text/javascript",
		"/lib.js":     "text/javascript",
		"/app.css":    "text/css",
		"/index.html": "text/html",
	}
	for path, ctype := range cases {
		rec := get(t, path)
		if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), ctype) {
			t.Errorf("%s -> %d %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := get(t, "/")
	h := rec.Header()
	if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "http") {
		t.Errorf("CSP = %q", csp)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" || h.Get("X-Frame-Options") != "DENY" {
		t.Errorf("headers = %v", h)
	}
}

func TestNoExternalRequests(t *testing.T) {
	for _, p := range []string{"/", "/app.js", "/lib.js", "/signin.js", "/app.css"} {
		body := get(t, p).Body.String()
		for _, bad := range []string{"http://", "https://", "//cdn", "@import"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s contains %q", p, bad)
			}
		}
	}
}

func TestGzipWhenAccepted(t *testing.T) {
	plain := get(t, "/app.js")
	if plain.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzip sent without Accept-Encoding")
	}
	rec := get(t, "/app.js", "Accept-Encoding", "gzip, br")
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("expected gzip")
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != plain.Body.String() {
		t.Fatal("gzip body differs from plain body")
	}
}

func TestETagRevalidation(t *testing.T) {
	first := get(t, "/app.css")
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	if rec := get(t, "/app.css", "If-None-Match", etag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("conditional GET = %d with %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestUnknownPathAndMethods(t *testing.T) {
	if rec := get(t, "/nope.txt"); rec.Code != 404 {
		t.Errorf("unknown path = %d", rec.Code)
	}
	if rec := get(t, "/app.test.mjs"); rec.Code != 404 {
		t.Errorf("test files must not be served: %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Errorf("POST = %d", rec.Code)
	}
}

// The first page load (HTML + CSS + JS modules) must stay small.
func TestInitialPayloadUnder30KB(t *testing.T) {
	total := 0
	for _, p := range []string{"/", "/app.css", "/app.js", "/lib.js", "/signin.js"} {
		total += get(t, p).Body.Len()
	}
	if total > 30*1024 {
		t.Fatalf("initial payload = %d bytes, want <= 30720", total)
	}
	t.Logf("initial payload: %d bytes uncompressed", total)
}
