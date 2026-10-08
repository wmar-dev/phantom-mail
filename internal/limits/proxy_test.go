package limits

import (
	"net/netip"
	"testing"
)

func prefixes(t *testing.T, ss ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, s := range ss {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func TestClientIP(t *testing.T) {
	trusted := prefixes(t, "10.0.0.0/8", "192.168.0.0/16")
	cases := []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"no proxy configured ignores header", "203.0.113.9:5555", []string{"1.2.3.4"}, nil, "203.0.113.9"},
		{"untrusted peer ignores header", "203.0.113.9:5555", []string{"1.2.3.4"}, trusted, "203.0.113.9"},
		{"trusted peer, single hop", "10.0.0.5:1234", []string{"198.51.100.7"}, trusted, "198.51.100.7"},
		{"trusted peer, no header", "10.0.0.5:1234", nil, trusted, "10.0.0.5"},
		{"chain of trusted proxies", "10.0.0.5:1234", []string{"198.51.100.7, 10.0.0.9, 192.168.1.1"}, trusted, "198.51.100.7"},
		{"spoofed leftmost entry is not trusted", "10.0.0.5:1234", []string{"6.6.6.6, 198.51.100.7"}, trusted, "198.51.100.7"},
		{"multiple header lines", "10.0.0.5:1234", []string{"6.6.6.6", "198.51.100.7"}, trusted, "198.51.100.7"},
		{"garbage entry falls back to peer", "10.0.0.5:1234", []string{"not-an-ip"}, trusted, "10.0.0.5"},
		{"all hops trusted returns leftmost", "10.0.0.5:1234", []string{"192.168.1.1, 10.1.1.1"}, trusted, "192.168.1.1"},
		{"ipv6 peer", "[2001:db8::1]:443", nil, trusted, "2001:db8::1"},
		{"remote without port", "203.0.113.9", nil, nil, "203.0.113.9"},
	}
	for _, c := range cases {
		if got := ClientIP(c.remote, c.xff, c.trusted); got != c.want {
			t.Errorf("%s: ClientIP = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsHTTPS(t *testing.T) {
	trusted := prefixes(t, "10.0.0.0/8")
	if !ForwardedHTTPS("10.0.0.5:1", "https", trusted) {
		t.Fatal("trusted proxy saying https should count")
	}
	if ForwardedHTTPS("203.0.113.9:1", "https", trusted) {
		t.Fatal("untrusted peer must not be able to claim https")
	}
	if ForwardedHTTPS("10.0.0.5:1", "http", trusted) {
		t.Fatal("http is not https")
	}
}
