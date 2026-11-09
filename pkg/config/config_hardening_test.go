package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// The new security fields are additive: old YAML still parses unchanged, and
// the new keys round-trip.
func TestSecurityConfigFieldsParse(t *testing.T) {
	var old Config
	if err := yaml.Unmarshal([]byte("agent:\n  watch:\n    webhook:\n      enabled: true\n      auth_token: abc\n"), &old); err != nil {
		t.Fatal(err)
	}
	wh := old.Agent.Watch.Webhook
	if !wh.Enabled || wh.AuthToken != "abc" || wh.HMACSecret != "" || wh.DisableLegacyAuth {
		t.Fatalf("legacy webhook config changed meaning: %+v", wh)
	}

	var c Config
	doc := `
agent:
  watch:
    webhook:
      enabled: true
      hmac_secret: s3cr3t-s3cr3t-s3cr3t
      timestamp_tolerance: 2m
      disable_legacy_auth: true
webui:
  enabled: true
  session_max_lifetime: 12h
  allowed_origins: ["https://dso.example.com"]
  allowed_hosts: ["dso.example.com"]
  trusted_proxies: ["10.0.0.0/8"]
`
	if err := yaml.Unmarshal([]byte(doc), &c); err != nil {
		t.Fatal(err)
	}
	w := c.Agent.Watch.Webhook
	if w.HMACSecret == "" || w.TimestampTolerance != "2m" || !w.DisableLegacyAuth {
		t.Errorf("webhook fields: %+v", w)
	}
	u := c.WebUI
	if u.SessionMaxLifetime != "12h" || len(u.AllowedOrigins) != 1 || len(u.AllowedHosts) != 1 || len(u.TrustedProxies) != 1 {
		t.Errorf("webui fields: %+v", u)
	}
}
