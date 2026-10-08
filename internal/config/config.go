// Package config loads settings from PM_* environment variables. Every
// setting has a local-friendly default so an empty environment is valid.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	Domains  []string
	HTTPAddr string
	SMTPAddr string
	DataDir  string
	Storage  string // "fs" or "memory"

	Retention       time.Duration
	JanitorInterval time.Duration

	MaxMessagesPerMailbox int
	MaxMailboxes          int
	MaxStreamsPerClient   int // concurrent event streams per client address
	MaxMessageBytes       int64
	MaxTotalBytes         int64

	// Rates are per minute. Each limiter allows a burst equal to its rate.
	RateSMTPConn    int // SMTP connections per remote address
	RateSMTPMessage int // SMTP messages per remote address
	RateMailbox     int // messages per mailbox
	RateHTTP        int // HTTP requests per client address
	RateAuthFail    int // failed sign-ins per client address

	APIToken       string
	TrustedProxies []netip.Prefix
	LogLevel       string
}

// Defaults returns the built-in settings.
func Defaults() Config {
	return Config{
		Domains:               []string{"localhost"},
		HTTPAddr:              ":8080",
		SMTPAddr:              ":2525",
		DataDir:               "./data",
		Storage:               "fs",
		Retention:             24 * time.Hour,
		JanitorInterval:       30 * time.Second,
		MaxMessagesPerMailbox: 100,
		MaxMailboxes:          10000,
		MaxStreamsPerClient:   100,
		MaxMessageBytes:       10 << 20,
		MaxTotalBytes:         1 << 30,
		RateSMTPConn:          600,
		RateSMTPMessage:       3000,
		RateMailbox:           600,
		RateHTTP:              6000,
		RateAuthFail:          10,
		LogLevel:              "info",
	}
}

// Load reads configuration through getenv (usually os.Getenv). Blank values
// fall back to defaults; all invalid values are reported together.
func Load(getenv func(string) string) (Config, error) {
	c := Defaults()
	var errs []error
	get := func(key string) (string, bool) {
		v := strings.TrimSpace(getenv(key))
		return v, v != ""
	}
	bad := func(key string, err error) { errs = append(errs, fmt.Errorf("%s: %w", key, err)) }

	if v, ok := get("PM_DOMAINS"); ok {
		var ds []string
		for _, d := range strings.Split(v, ",") {
			d = strings.ToLower(strings.TrimSpace(d))
			if d == "" {
				continue
			}
			if !validHostname(d) {
				bad("PM_DOMAINS", fmt.Errorf("%q is not a valid host name", d))
				continue
			}
			ds = append(ds, d)
		}
		if len(ds) > 0 {
			c.Domains = ds
		}
	}
	if v, ok := get("PM_HTTP_ADDR"); ok {
		c.HTTPAddr = v
	}
	if v, ok := get("PM_SMTP_ADDR"); ok {
		c.SMTPAddr = v
	}
	if v, ok := get("PM_DATA_DIR"); ok {
		c.DataDir = v
	}
	if v, ok := get("PM_STORAGE"); ok {
		if v != "fs" && v != "memory" {
			bad("PM_STORAGE", fmt.Errorf("%q must be \"fs\" or \"memory\"", v))
		} else {
			c.Storage = v
		}
	}
	dur := func(key string, dst *time.Duration) {
		if v, ok := get(key); ok {
			d, err := time.ParseDuration(v)
			switch {
			case err != nil:
				bad(key, fmt.Errorf("%q is not a duration such as 90m or 24h", v))
			case d <= 0:
				bad(key, fmt.Errorf("%q must be positive", v))
			default:
				*dst = d
			}
		}
	}
	dur("PM_RETENTION", &c.Retention)
	dur("PM_JANITOR_INTERVAL", &c.JanitorInterval)

	num := func(key string, dst *int) {
		if v, ok := get(key); ok {
			n, err := strconv.Atoi(v)
			switch {
			case err != nil:
				bad(key, fmt.Errorf("%q is not a whole number", v))
			case n <= 0:
				bad(key, fmt.Errorf("%q must be positive", v))
			default:
				*dst = n
			}
		}
	}
	num("PM_MAX_MESSAGES_PER_MAILBOX", &c.MaxMessagesPerMailbox)
	num("PM_MAX_MAILBOXES", &c.MaxMailboxes)
	num("PM_MAX_STREAMS_PER_CLIENT", &c.MaxStreamsPerClient)
	num("PM_RATE_SMTP_CONN", &c.RateSMTPConn)
	num("PM_RATE_SMTP_MESSAGE", &c.RateSMTPMessage)
	num("PM_RATE_MAILBOX", &c.RateMailbox)
	num("PM_RATE_HTTP", &c.RateHTTP)
	num("PM_RATE_AUTH_FAIL", &c.RateAuthFail)

	size := func(key string, dst *int64) {
		if v, ok := get(key); ok {
			n, err := parseBytes(v)
			switch {
			case err != nil:
				bad(key, err)
			case n <= 0:
				bad(key, fmt.Errorf("%q must be positive", v))
			default:
				*dst = n
			}
		}
	}
	size("PM_MAX_MESSAGE_BYTES", &c.MaxMessageBytes)
	size("PM_MAX_TOTAL_BYTES", &c.MaxTotalBytes)

	if v, ok := get("PM_API_TOKEN"); ok {
		c.APIToken = v
	}
	if v, ok := get("PM_TRUSTED_PROXIES"); ok {
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			p, err := parsePrefix(s)
			if err != nil {
				bad("PM_TRUSTED_PROXIES", fmt.Errorf("%q is not an IP address or CIDR range", s))
				continue
			}
			c.TrustedProxies = append(c.TrustedProxies, p)
		}
	}
	if v, ok := get("PM_LOG_LEVEL"); ok {
		switch strings.ToLower(v) {
		case "debug", "info", "warn", "error":
			c.LogLevel = strings.ToLower(v)
		default:
			bad("PM_LOG_LEVEL", fmt.Errorf("%q must be debug, info, warn, or error", v))
		}
	}
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return c, nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

var byteUnits = []struct {
	suffix string
	mult   int64
}{
	// Longest suffixes first so "MiB" is not read as "B".
	{"kib", 1 << 10}, {"mib", 1 << 20}, {"gib", 1 << 30},
	{"kb", 1000}, {"mb", 1000 * 1000}, {"gb", 1000 * 1000 * 1000},
	{"b", 1},
}

func parseBytes(s string) (int64, error) {
	orig := s
	s = strings.ToLower(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range byteUnits {
		if strings.HasSuffix(s, u.suffix) {
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			mult = u.mult
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size such as 10485760, 10MiB, or 1GB", orig)
	}
	return n * mult, nil
}

func validHostname(h string) bool {
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
