package mailbox

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"alice", "alice", true},
		{"Alice", "alice", true},
		{"ALICE.Smith_1-x", "alice.smith_1-x", true},
		{"alice+shop", "alice", true},
		{"Alice+a+b", "alice", true},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), "", false},
		{"", "", false},
		{"+tag", "", false},
		{"../x", "", false},
		{"a/b", "", false},
		{`a\b`, "", false},
		{"a b", "", false},
		{"a@b", "", false},
		{"..", "", false},
		{".", "", false},
		{"ü", "", false},
		{"a\x00b", "", false},
		{"alice\n", "", false},
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if (err == nil) != c.ok {
			t.Errorf("Normalize(%q) err = %v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidRejectsNonCanonical(t *testing.T) {
	if !Valid("alice") {
		t.Error("alice should be valid")
	}
	for _, bad := range []string{"Alice", "alice+x", "../x", ""} {
		if Valid(bad) {
			t.Errorf("Valid(%q) = true", bad)
		}
	}
}

func TestSplitAddress(t *testing.T) {
	cases := []struct {
		in, local, domain string
		ok                bool
	}{
		{"alice@localhost", "alice", "localhost", true},
		{"<alice@localhost>", "alice", "localhost", true},
		{"Alice+Shop@Mail.Example.COM", "Alice+Shop", "mail.example.com", true},
		{"<>", "", "", false},
		{"alice", "", "", false},
		{"@localhost", "", "", false},
		{"alice@", "", "", false},
	}
	for _, c := range cases {
		l, d, ok := SplitAddress(c.in)
		if ok != c.ok || l != c.local || d != c.domain {
			t.Errorf("SplitAddress(%q) = %q,%q,%v want %q,%q,%v", c.in, l, d, ok, c.local, c.domain, c.ok)
		}
	}
}

func TestDomainServed(t *testing.T) {
	domains := []string{"mail.example.com", "localhost"}
	for _, d := range []string{"mail.example.com", "MAIL.EXAMPLE.COM", "localhost"} {
		if !DomainServed(domains, d) {
			t.Errorf("DomainServed(%q) = false", d)
		}
	}
	for _, d := range []string{"example.com", "evil.mail.example.com", "", "localhost.evil"} {
		if DomainServed(domains, d) {
			t.Errorf("DomainServed(%q) = true", d)
		}
	}
}
