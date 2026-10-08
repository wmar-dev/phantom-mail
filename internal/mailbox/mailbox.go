// Package mailbox normalizes mailbox names and matches served domains.
package mailbox

import (
	"errors"
	"strings"
)

// MaxLen is the longest allowed mailbox name.
const MaxLen = 64

// ErrInvalid is returned for names that are not valid mailbox names.
var ErrInvalid = errors.New("invalid mailbox name")

// Normalize lowercases a local part, drops any "+tag" suffix, and validates
// the result against ^[a-z0-9._-]{1,64}$ (no "." or ".." names). The strict
// character set also keeps names safe to use as directory names.
func Normalize(local string) (string, error) {
	local = strings.ToLower(local)
	if i := strings.IndexByte(local, '+'); i >= 0 {
		local = local[:i]
	}
	if !validChars(local) {
		return "", ErrInvalid
	}
	return local, nil
}

// Valid reports whether name is already in canonical form.
func Valid(name string) bool {
	n, err := Normalize(name)
	return err == nil && n == name
}

func validChars(s string) bool {
	if s == "" || len(s) > MaxLen || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// SplitAddress splits "<local@domain>" or "local@domain" into its local part
// and lowercased domain. The local part keeps its original case.
func SplitAddress(addr string) (local, domain string, ok bool) {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "<")
	addr = strings.TrimSuffix(addr, ">")
	i := strings.LastIndexByte(addr, '@')
	if i <= 0 || i == len(addr)-1 {
		return "", "", false
	}
	return addr[:i], strings.ToLower(addr[i+1:]), true
}

// DomainServed reports whether domain is one of the served domains.
func DomainServed(domains []string, domain string) bool {
	domain = strings.ToLower(domain)
	if domain == "" {
		return false
	}
	for _, d := range domains {
		if strings.ToLower(d) == domain {
			return true
		}
	}
	return false
}
