package limits

import (
	"net"
	"net/netip"
	"strings"
)

func peerAddr(remote string) string {
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP returns the address of the real client. X-Forwarded-For is only
// honored when the direct peer is in trusted; the chain is then walked from
// the right and the first address that is not itself a trusted proxy wins,
// so entries a client prepends cannot be spoofed.
func ClientIP(remote string, xff []string, trusted []netip.Prefix) string {
	peer := peerAddr(remote)
	pa, err := netip.ParseAddr(peer)
	if err != nil || len(trusted) == 0 || !isTrusted(pa, trusted) {
		return peer
	}
	var chain []string
	for _, v := range xff {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				chain = append(chain, part)
			}
		}
	}
	if len(chain) == 0 {
		return peer
	}
	for i := len(chain) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(chain[i])
		if err != nil {
			return peer
		}
		if !isTrusted(a, trusted) {
			return a.Unmap().String()
		}
		if i == 0 {
			return a.Unmap().String()
		}
	}
	return peer
}

// ForwardedHTTPS reports whether the request arrived over HTTPS according to
// a trusted proxy's X-Forwarded-Proto header.
func ForwardedHTTPS(remote, proto string, trusted []netip.Prefix) bool {
	pa, err := netip.ParseAddr(peerAddr(remote))
	if err != nil || !isTrusted(pa, trusted) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}
