package httpapi

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/limits"
)

const token = "s3cret-token-value"

func withToken(c *Config) { c.APIToken = token }

func bearer() map[string]string { return map[string]string{"Authorization": "Bearer " + token} }

func signIn(t *testing.T, e *env, tok string, hdr map[string]string) *http.Response {
	t.Helper()
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range hdr {
		h[k] = v
	}
	return e.do("POST", "/api/v1/session", h, strings.NewReader(`{"token":"`+tok+`"}`))
}

func sessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == "pm_session" {
			return c
		}
	}
	t.Fatalf("no pm_session cookie in %v", resp.Header["Set-Cookie"])
	return nil
}

func TestNoTokenConfiguredMeansOpenAccess(t *testing.T) {
	e := newEnv(t)
	if resp := e.get("/api/v1/mailboxes/alice/messages"); resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp := signIn(t, e, "anything", nil)
	if resp.StatusCode != http.StatusNoContent || len(resp.Cookies()) != 0 {
		t.Fatalf("session with no token configured = %d, cookies %v", resp.StatusCode, resp.Cookies())
	}
}

func TestBearerTokenProtectsTheAPI(t *testing.T) {
	e := newEnv(t, withToken)
	e.deliver("alice", plain("x", "y"))
	wantError(t, e.get("/api/v1/mailboxes/alice/messages"), 401, "unauthorized")
	wantError(t, e.do("GET", "/api/v1/mailboxes/alice/messages", map[string]string{"Authorization": "Bearer wrong"}, nil), 401, "unauthorized")
	wantError(t, e.do("GET", "/api/v1/mailboxes/alice/messages", map[string]string{"Authorization": token}, nil), 401, "unauthorized")
	wantError(t, e.get("/api/v1/nope"), 401, "unauthorized")
	if resp := e.do("GET", "/api/v1/mailboxes/alice/messages", bearer(), nil); resp.StatusCode != 200 {
		t.Fatalf("valid bearer status = %d", resp.StatusCode)
	}
	if resp := e.do("DELETE", "/api/v1/mailboxes/alice/messages", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated delete status = %d", resp.StatusCode)
	}
}

func TestOpenEndpointsStayOpen(t *testing.T) {
	e := newEnv(t, withToken, func(c *Config) {
		c.UI = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ui") })
	})
	for _, p := range []string{"/healthz", "/openapi.yaml", "/", "/app.js"} {
		if resp := e.get(p); resp.StatusCode != 200 {
			t.Errorf("GET %s = %d without credentials", p, resp.StatusCode)
		}
	}
}

func TestTokenInQueryStringIsNotAccepted(t *testing.T) {
	e := newEnv(t, withToken)
	wantError(t, e.get("/api/v1/mailboxes/alice/messages?token="+token), 401, "unauthorized")
	wantError(t, e.get("/api/v1/mailboxes/alice/events?access_token="+token), 401, "unauthorized")
}

func TestSessionCookieAuthorizesBrowserRequests(t *testing.T) {
	e := newEnv(t, withToken)
	id := e.deliver("alice", []byte(attachmentMsg))

	if resp := signIn(t, e, "wrong-token", nil); resp.StatusCode != 401 || len(resp.Cookies()) != 0 {
		t.Fatalf("wrong token status = %d", resp.StatusCode)
	}
	resp := signIn(t, e, token, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("sign-in status = %d", resp.StatusCode)
	}
	c := sessionCookie(t, resp)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Errorf("cookie attributes = %+v", c)
	}
	if c.Secure {
		t.Error("cookie must not be Secure over plain HTTP")
	}
	if c.MaxAge != 0 || !c.Expires.IsZero() {
		t.Error("session cookie should last for the browser session only")
	}
	if strings.Contains(c.Value, token) || c.Value == "" {
		t.Errorf("cookie value must not contain the raw token: %q", c.Value)
	}
	for _, v := range resp.Header {
		if strings.Contains(strings.Join(v, ","), token) {
			t.Error("token echoed back in a response header")
		}
	}

	cookie := map[string]string{"Cookie": "pm_session=" + c.Value}
	base := "/api/v1/mailboxes/alice/messages"
	for _, p := range []string{base, base + "/" + id, base + "/" + id + "/html", base + "/" + id + "/attachments/0"} {
		if r := e.do("GET", p, cookie, nil); r.StatusCode != 200 {
			t.Errorf("GET %s with session = %d", p, r.StatusCode)
		}
	}
	// Event streams (EventSource cannot send headers) work with the cookie.
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1/mailboxes/alice/events", nil)
	req.Header.Set("Cookie", "pm_session="+c.Value)
	sse, err := http.DefaultClient.Do(req)
	if err != nil || sse.StatusCode != 200 {
		t.Fatalf("SSE with cookie: %v %v", sse, err)
	}
	sse.Body.Close()
	if r := e.do("DELETE", base+"/"+id, cookie, nil); r.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE with session = %d", r.StatusCode)
	}

	wantError(t, e.do("GET", base, map[string]string{"Cookie": "pm_session=forged"}, nil), 401, "unauthorized")
	wantError(t, e.do("GET", base, map[string]string{"Cookie": "pm_session=" + token}, nil), 401, "unauthorized")
}

func TestSessionSignOutClearsTheCookie(t *testing.T) {
	e := newEnv(t, withToken)
	resp := e.do("DELETE", "/api/v1/session", nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	c := sessionCookie(t, resp)
	if c.MaxAge >= 0 && !c.Expires.Before(time.Now()) {
		t.Errorf("sign-out cookie is not expired: %+v", c)
	}
}

func TestSessionRequestValidation(t *testing.T) {
	e := newEnv(t, withToken)
	cases := []string{``, `not json`, `{}`, `{"token":123}`, `{"token":""}`, strings.Repeat("x", 10000)}
	for _, body := range cases {
		resp := e.do("POST", "/api/v1/session", map[string]string{"Content-Type": "application/json"}, strings.NewReader(body))
		if resp.StatusCode != 400 && resp.StatusCode != 401 {
			t.Errorf("body %.20q -> %d, want 400 or 401", body, resp.StatusCode)
		}
	}
}

func TestFailedSignInsAreRateLimited(t *testing.T) {
	clk := clock.NewFake(time.Now())
	e := newEnv(t, withToken, func(c *Config) {
		c.AuthFailLimiter = limits.New(3, clk)
		c.Clock = clk
	})
	for i := 0; i < 3; i++ {
		if resp := signIn(t, e, "wrong", nil); resp.StatusCode != 401 {
			t.Fatalf("attempt %d = %d", i, resp.StatusCode)
		}
	}
	wantError(t, signIn(t, e, "wrong", nil), 429, "rate_limited")
	// Even the correct token is refused while locked out, so guessing cannot continue.
	wantError(t, signIn(t, e, token, nil), 429, "rate_limited")
	clk.Advance(2 * time.Minute)
	if resp := signIn(t, e, token, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("after the lockout expired = %d", resp.StatusCode)
	}
}

func TestSuccessfulSignInsDoNotConsumeTheFailureBudget(t *testing.T) {
	e := newEnv(t, withToken, func(c *Config) { c.AuthFailLimiter = limits.New(2, clock.Real{}) })
	for i := 0; i < 10; i++ {
		if resp := signIn(t, e, token, nil); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("sign-in %d = %d", i, resp.StatusCode)
		}
	}
}

func proxyConfig(c *Config) {
	c.APIToken = token
	c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
}

func TestSecureCookieBehindTrustedHTTPSProxy(t *testing.T) {
	e := newEnv(t, proxyConfig)
	c := sessionCookie(t, signIn(t, e, token, map[string]string{"X-Forwarded-Proto": "https"}))
	if !c.Secure {
		t.Error("cookie should be Secure when a trusted proxy says the request was HTTPS")
	}
	c = sessionCookie(t, signIn(t, e, token, map[string]string{"X-Forwarded-Proto": "http"}))
	if c.Secure {
		t.Error("cookie must not be Secure for plain HTTP")
	}
}

func TestForwardedProtoIgnoredFromUntrustedPeers(t *testing.T) {
	e := newEnv(t, withToken) // no trusted proxies configured
	c := sessionCookie(t, signIn(t, e, token, map[string]string{"X-Forwarded-Proto": "https"}))
	if c.Secure {
		t.Error("an untrusted peer must not be able to claim HTTPS")
	}
}

func TestHTTPRateLimit(t *testing.T) {
	clk := clock.NewFake(time.Now())
	e := newEnv(t, func(c *Config) {
		c.RateLimiter = limits.New(5, clk)
		c.Clock = clk
	})
	for i := 0; i < 5; i++ {
		if resp := e.get("/api/v1/mailboxes/alice/messages"); resp.StatusCode != 200 {
			t.Fatalf("request %d = %d", i, resp.StatusCode)
		}
	}
	resp := e.get("/api/v1/mailboxes/alice/messages")
	wantError(t, resp, 429, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 should carry Retry-After")
	}
	if r := e.get("/healthz"); r.StatusCode != 200 {
		t.Errorf("health checks must be exempt from rate limiting, got %d", r.StatusCode)
	}
}

func TestRateLimitUsesForwardedClientBehindTrustedProxy(t *testing.T) {
	clk := clock.NewFake(time.Now())
	e := newEnv(t, func(c *Config) {
		c.RateLimiter = limits.New(2, clk)
		c.Clock = clk
		c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	})
	get := func(client string) int {
		return e.do("GET", "/api/v1/mailboxes/alice/messages", map[string]string{"X-Forwarded-For": client}, nil).StatusCode
	}
	for i := 0; i < 2; i++ {
		get("198.51.100.1")
	}
	if get("198.51.100.1") != 429 {
		t.Fatal("client A should be limited")
	}
	if get("198.51.100.2") != 200 {
		t.Fatal("client B must not share client A's budget")
	}
}

func TestTokenNeverAppearsInLogs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := newEnv(t, withToken, func(c *Config) { c.Logger = log })
	e.get("/api/v1/mailboxes/alice/messages?token=" + token)
	e.do("GET", "/api/v1/mailboxes/alice/messages", bearer(), nil)
	signIn(t, e, token, nil)
	signIn(t, e, "also-"+token, nil)
	e.do("GET", "/api/v1/mailboxes/alice/messages", map[string]string{"Cookie": "pm_session=abc"}, nil)
	if buf.Len() == 0 {
		t.Fatal("expected request logs")
	}
	if strings.Contains(buf.String(), token) {
		t.Fatalf("token leaked into logs:\n%s", buf.String())
	}
}
