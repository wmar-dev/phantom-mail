package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Domains) != 1 || c.Domains[0] != "localhost" {
		t.Errorf("domains = %v", c.Domains)
	}
	if c.HTTPAddr != ":8080" || c.SMTPAddr != ":2525" {
		t.Errorf("addrs = %q %q", c.HTTPAddr, c.SMTPAddr)
	}
	if c.DataDir != "./data" || c.Storage != "fs" {
		t.Errorf("storage = %q %q", c.DataDir, c.Storage)
	}
	if c.Retention != 24*time.Hour || c.JanitorInterval != 30*time.Second {
		t.Errorf("durations = %v %v", c.Retention, c.JanitorInterval)
	}
	if c.MaxMessagesPerMailbox != 100 || c.MaxMailboxes != 10000 || c.MaxStreamsPerClient != 100 {
		t.Errorf("caps = %d %d %d", c.MaxMessagesPerMailbox, c.MaxMailboxes, c.MaxStreamsPerClient)
	}
	if c.MaxMessageBytes != 10<<20 || c.MaxTotalBytes != 1<<30 {
		t.Errorf("bytes = %d %d", c.MaxMessageBytes, c.MaxTotalBytes)
	}
	if c.APIToken != "" || len(c.TrustedProxies) != 0 || c.LogLevel != "info" {
		t.Errorf("misc = %q %v %q", c.APIToken, c.TrustedProxies, c.LogLevel)
	}
	if c.RateSMTPConn <= 0 || c.RateSMTPMessage <= 0 || c.RateMailbox <= 0 || c.RateHTTP <= 0 || c.RateAuthFail <= 0 {
		t.Errorf("rates not positive: %+v", c)
	}
}

func TestEveryVariable(t *testing.T) {
	c, err := Load(env(map[string]string{
		"PM_DOMAINS":                  " Mail.Example.com , second.test ",
		"PM_HTTP_ADDR":                "127.0.0.1:9000",
		"PM_SMTP_ADDR":                "127.0.0.1:2500",
		"PM_DATA_DIR":                 "/var/lib/pm",
		"PM_STORAGE":                  "memory",
		"PM_RETENTION":                "90m",
		"PM_JANITOR_INTERVAL":         "5s",
		"PM_MAX_MESSAGES_PER_MAILBOX": "7",
		"PM_MAX_MAILBOXES":            "9",
		"PM_MAX_STREAMS_PER_CLIENT":   "17",
		"PM_MAX_MESSAGE_BYTES":        "2MiB",
		"PM_MAX_TOTAL_BYTES":          "512MB",
		"PM_RATE_SMTP_CONN":           "11",
		"PM_RATE_SMTP_MESSAGE":        "12",
		"PM_RATE_MAILBOX":             "13",
		"PM_RATE_HTTP":                "14",
		"PM_RATE_AUTH_FAIL":           "15",
		"PM_API_TOKEN":                "s3cret",
		"PM_TRUSTED_PROXIES":          "10.0.0.0/8, 192.168.1.5",
		"PM_LOG_LEVEL":                "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Domains, ",") != "mail.example.com,second.test" {
		t.Errorf("domains = %v", c.Domains)
	}
	if c.HTTPAddr != "127.0.0.1:9000" || c.SMTPAddr != "127.0.0.1:2500" || c.DataDir != "/var/lib/pm" || c.Storage != "memory" {
		t.Errorf("basic = %+v", c)
	}
	if c.Retention != 90*time.Minute || c.JanitorInterval != 5*time.Second {
		t.Errorf("durations = %v %v", c.Retention, c.JanitorInterval)
	}
	if c.MaxMessagesPerMailbox != 7 || c.MaxMailboxes != 9 || c.MaxStreamsPerClient != 17 {
		t.Errorf("caps = %d %d %d", c.MaxMessagesPerMailbox, c.MaxMailboxes, c.MaxStreamsPerClient)
	}
	if c.MaxMessageBytes != 2<<20 || c.MaxTotalBytes != 512*1000*1000 {
		t.Errorf("bytes = %d %d", c.MaxMessageBytes, c.MaxTotalBytes)
	}
	if c.RateSMTPConn != 11 || c.RateSMTPMessage != 12 || c.RateMailbox != 13 || c.RateHTTP != 14 || c.RateAuthFail != 15 {
		t.Errorf("rates = %+v", c)
	}
	if c.APIToken != "s3cret" || c.LogLevel != "debug" {
		t.Errorf("token/log = %q %q", c.APIToken, c.LogLevel)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[0].String() != "10.0.0.0/8" || c.TrustedProxies[1].String() != "192.168.1.5/32" {
		t.Errorf("proxies = %v", c.TrustedProxies)
	}
}

func TestBlankValuesUseDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"PM_DOMAINS": "", "PM_RETENTION": "  ", "PM_HTTP_ADDR": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Domains[0] != "localhost" || c.Retention != 24*time.Hour || c.HTTPAddr != ":8080" {
		t.Errorf("blank values should fall back to defaults: %+v", c)
	}
}

func TestInvalidValuesAreRejectedWithClearErrors(t *testing.T) {
	cases := []struct{ key, val string }{
		{"PM_RETENTION", "tomorrow"},
		{"PM_RETENTION", "-5m"},
		{"PM_MAX_MESSAGES_PER_MAILBOX", "zero"},
		{"PM_MAX_MESSAGES_PER_MAILBOX", "0"},
		{"PM_MAX_MAILBOXES", "-1"},
		{"PM_MAX_STREAMS_PER_CLIENT", "0"},
		{"PM_MAX_MESSAGE_BYTES", "10 furlongs"},
		{"PM_MAX_TOTAL_BYTES", "0"},
		{"PM_STORAGE", "s3"},
		{"PM_LOG_LEVEL", "chatty"},
		{"PM_TRUSTED_PROXIES", "not-an-ip"},
		{"PM_DOMAINS", "bad domain!"},
		{"PM_DOMAINS", "-bad.example.com"},
		{"PM_RATE_HTTP", "x"},
		{"PM_JANITOR_INTERVAL", "0s"},
	}
	for _, c := range cases {
		_, err := Load(env(map[string]string{c.key: c.val}))
		if err == nil {
			t.Errorf("%s=%q accepted", c.key, c.val)
			continue
		}
		if !strings.Contains(err.Error(), c.key) {
			t.Errorf("%s=%q error %q does not name the variable", c.key, c.val, err)
		}
	}
}

func TestAllErrorsReportedTogether(t *testing.T) {
	_, err := Load(env(map[string]string{"PM_RETENTION": "x", "PM_STORAGE": "y"}))
	if err == nil || !strings.Contains(err.Error(), "PM_RETENTION") || !strings.Contains(err.Error(), "PM_STORAGE") {
		t.Fatalf("err = %v", err)
	}
}
