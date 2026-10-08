package config

import "testing"

// A production-style environment is honored purely through configuration:
// no code change is needed to move from the local defaults to a cloud setup.
func TestProductionStyleEnvironment(t *testing.T) {
	c, err := Load(env(map[string]string{
		"PM_DOMAINS":         "mail.example.com",
		"PM_HTTP_ADDR":       ":8080",
		"PM_SMTP_ADDR":       ":2525",
		"PM_DATA_DIR":        "/data",
		"PM_API_TOKEN":       "token-123",
		"PM_TRUSTED_PROXIES": "10.0.0.0/8",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Domains[0] != "mail.example.com" || c.DataDir != "/data" || c.APIToken != "token-123" {
		t.Fatalf("production env not honored: %+v", c)
	}
	if len(c.TrustedProxies) != 1 {
		t.Fatalf("trusted proxies = %v", c.TrustedProxies)
	}
}

func TestLocalDefaultsNeedNoEnvironment(t *testing.T) {
	c, err := Load(env(map[string]string{}))
	if err != nil {
		t.Fatalf("empty environment must be valid: %v", err)
	}
	if c.Domains[0] != "localhost" || c.Storage != "fs" {
		t.Fatalf("unexpected local defaults: %+v", c)
	}
}
