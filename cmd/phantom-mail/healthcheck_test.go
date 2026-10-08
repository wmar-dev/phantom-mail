package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthURL(t *testing.T) {
	cases := map[string]string{
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/healthz",
		"[::]:9000":      "http://127.0.0.1:9000/healthz",
		"127.0.0.1:8099": "http://127.0.0.1:8099/healthz",
		"localhost:80":   "http://localhost:80/healthz",
		"garbage":        "http://127.0.0.1:8080/healthz",
	}
	for in, want := range cases {
		if got := healthURL(in); got != want {
			t.Errorf("healthURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func serve(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestProbe(t *testing.T) {
	good := serve(200, `{"status":"ok","messages":0}`)
	defer good.Close()
	if err := probe(good.URL+"/healthz", time.Second); err != nil {
		t.Errorf("healthy server reported as unhealthy: %v", err)
	}
	for name, srv := range map[string]*httptest.Server{
		"500":         serve(500, `{"status":"ok"}`),
		"wrong body":  serve(200, `{"status":"starting"}`),
		"empty body":  serve(200, ``),
		"not healthy": serve(503, `down`),
	} {
		if err := probe(srv.URL+"/healthz", time.Second); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		srv.Close()
	}
	if err := probe("http://127.0.0.1:1/healthz", 500*time.Millisecond); err == nil {
		t.Error("unreachable server reported as healthy")
	}
}

func TestHealthcheckCommandExitCodes(t *testing.T) {
	good := serve(200, `{"status":"ok"}`)
	defer good.Close()
	env := func(addr string) func(string) string {
		return func(k string) string {
			if k == "PM_HTTP_ADDR" {
				return addr
			}
			return ""
		}
	}
	if code := healthcheckCommand(env(strings.TrimPrefix(good.URL, "http://"))); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if code := healthcheckCommand(env("127.0.0.1:1")); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunRejectsUnknownCommands(t *testing.T) {
	if code := run([]string{"frobnicate"}); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
