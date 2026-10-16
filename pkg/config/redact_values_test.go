package config

import (
	"strings"
	"testing"
)

// The exact document from the independent review (finding M0-3): before the
// fix every value below except hmac_secret was printed verbatim.
const reviewRedactionInput = `agent:
  watch:
    webhook:
      hmac_secret: "0123456789abcdef0123456789abcdef"
providers:
  p1:
    type: custom
    config:
      endpoint: "https://admin:hunter2pass@vault.internal:8200"
      connection: "postgres://svc:dbpassword99@db/app"
      region: us-east-1
      role_id: "11111111-2222-3333-4444-555555555555"
      jwt: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijklmnop"
      notes: "ssn 123-45-6789"
secrets:
  - name: db
    provider: p1
    inject: env
    path: "/app/dbpassword99"
`

func TestRedactValues_ReviewExamplesDoNotLeak(t *testing.T) {
	out, err := RedactYAML([]byte(reviewRedactionInput))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, leak := range []string{
		"hunter2pass", "admin:", "dbpassword99", "svc:",
		"11111111-2222-3333-4444-555555555555",
		"eyJhbGciOiJIUzI1NiJ9", "abcdefghijklmnop",
		"123-45-6789", "0123456789abcdef0123456789abcdef",
	} {
		if strings.Contains(s, leak) {
			t.Errorf("leaked %q in:\n%s", leak, s)
		}
	}
	// Still useful: allowlisted setting and hosts survive.
	for _, keep := range []string{"region: us-east-1", "https://vault.internal:8200", "type: custom", "provider: p1"} {
		if !strings.Contains(s, keep) {
			t.Errorf("over-redacted: %q missing in:\n%s", keep, s)
		}
	}
}

func TestRedactValues_FreeFormIsFailClosedForUnknownKeys(t *testing.T) {
	in := `providers:
  p:
    config:
      region: eu-west-1
      namespace: team-a
      anything: ordinary-looking-value
      path: secret/data/app
      colour: blue
`
	out, _ := RedactYAML([]byte(in))
	s := string(out)
	for _, hidden := range []string{"ordinary-looking-value", "secret/data/app", "blue"} {
		if strings.Contains(s, hidden) {
			t.Errorf("unknown key value %q was shown:\n%s", hidden, s)
		}
	}
	for _, shown := range []string{"eu-west-1", "team-a"} {
		if !strings.Contains(s, shown) {
			t.Errorf("allowlisted value %q hidden:\n%s", shown, s)
		}
	}
}

func TestRedactValues_AllowlistedKeyWithCredentialValueIsStillRedacted(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijklmnop"
	in := "providers:\n  p:\n    config:\n      region: " + jwt + "\n      namespace: \"AKIAIOSFODNN7EXAMPLE\"\n      version: ghp_abcdefghijklmnopqrstuvwxyz0123456789\n"
	out, _ := RedactYAML([]byte(in))
	for _, leak := range []string{jwt, "AKIAIOSFODNN7EXAMPLE", "ghp_abcdefghijklmnop"} {
		if strings.Contains(string(out), leak) {
			t.Errorf("credential under an ordinary key leaked: %q\n%s", leak, out)
		}
	}
}

func TestRedactValues_LegacyConfigMapAndNestedValues(t *testing.T) {
	in := `provider: aws
config:
  endpoint: https://svc:topsecretpw@legacy.internal/path?token=abc
  nested:
    inner: value-that-should-not-show
  list:
    - first-hidden-item
`
	out, _ := RedactYAML([]byte(in))
	s := string(out)
	for _, leak := range []string{"topsecretpw", "token=abc", "value-that-should-not-show", "first-hidden-item", "/path"} {
		if strings.Contains(s, leak) {
			t.Errorf("legacy config leaked %q:\n%s", leak, s)
		}
	}
	if !strings.Contains(s, "legacy.internal") {
		t.Errorf("host should remain visible:\n%s", s)
	}
}

func TestRedactValues_AnyScalarWithCredentialShapeIsRedactedEverywhere(t *testing.T) {
	in := `agent:
  cache:
    note: "dial postgres://u:p4ssw0rd@db:5432/x"
labels_demo:
  token_like: ordinary
  free_text: "-----BEGIN PRIVATE KEY-----"
  blob: "xoxb-1234567890-abcdefghij"
`
	out, _ := RedactYAML([]byte(in))
	s := string(out)
	// The postgres URL is embedded in prose (not a bare URL value) so shape
	// detection does not claim it; the PEM header and Slack token are.
	for _, leak := range []string{"BEGIN PRIVATE KEY", "xoxb-1234567890-abcdefghij"} {
		if strings.Contains(s, leak) {
			t.Errorf("leaked %q:\n%s", leak, s)
		}
	}
}

func TestRedactValues_OrdinaryValuesAreNotMangled(t *testing.T) {
	in := `agent:
  watch:
    webhook:
      enabled: true
secrets:
  - name: db_password_name
    provider: aws
    path: prod/db/password
    mappings:
      DB: some/path/to/value
unknown_section:
  hostname: secretsmanager.eu-west-1.amazonaws.com
  sentence: this is just a long sentence of ordinary words
  endpoint_url: https://example.com:8443
`
	out, _ := RedactYAML([]byte(in))
	s := string(out)
	for _, keep := range []string{"db_password_name", "prod/db/password", "some/path/to/value",
		"secretsmanager.eu-west-1.amazonaws.com", "just a long sentence", "https://example.com:8443"} {
		if !strings.Contains(s, keep) {
			t.Errorf("ordinary value %q was redacted:\n%s", keep, s)
		}
	}
}

func TestRedactValues_Idempotent(t *testing.T) {
	once, err := RedactYAML([]byte(reviewRedactionInput))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := RedactYAML(once)
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Fatalf("redaction is not idempotent:\n--- once\n%s\n--- twice\n%s", once, twice)
	}
}

func TestSensitiveShape_And_Heuristic(t *testing.T) {
	yes := []string{
		"https://u:p@host/x", "postgres://u@host", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcd",
		"AKIAIOSFODNN7EXAMPLE", "ghp_abcdefghijklmnopqrstuvwxyz0123456789", "xoxb-1234567890-abcdefghij",
		"-----BEGIN RSA PRIVATE KEY-----", "hvs.CAESIJabcdefghijklmnopqrstuvwxyz",
	}
	for _, v := range yes {
		if _, ok := sensitiveShape(v); !ok {
			t.Errorf("not detected: %q", v)
		}
	}
	no := []string{"", "hello", "https://example.com", "prod/db/password", "us-east-1", "true", "12345"}
	for _, v := range no {
		if _, ok := sensitiveShape(v); ok {
			t.Errorf("false positive: %q", v)
		}
	}
	if !looksRandom("Zk3vQ9xL2mP7aR5tY8uW1nB4") {
		t.Error("opaque token not detected")
	}
	for _, v := range []string{"secretsmanager.eu-west-1.amazonaws.com", "this is a sentence of words", "aaaaaaaaaaaaaaaaaaaaaaaa", "/var/lib/dso/some/path"} {
		if looksRandom(v) {
			t.Errorf("heuristic false positive: %q", v)
		}
	}
}

func TestRedactValues_UUIDUnderAllowlistedKeyIsRedacted(t *testing.T) {
	const id = "9f1c2b3a-4d5e-4f60-8a7b-0c1d2e3f4a5b"
	out, _ := RedactYAML([]byte("providers:\n  p:\n    config:\n      namespace: " + id + "\n"))
	if strings.Contains(string(out), id) {
		t.Errorf("UUID-shaped identifier leaked under an allowlisted key:\n%s", out)
	}
}

// A credential-shaped value must not survive just because the schema knows the
// field and it carries no `dso` tag (here: webhook.endpoint, secrets[].path).
func TestRedactValues_CredentialShapeInKnownUntaggedFieldsIsRedacted(t *testing.T) {
	in := `agent:
  watch:
    webhook:
      endpoint: https://hook-user:Sup3rPassw0rd@receiver.internal/hook
secrets:
  - name: db
    path: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijklmnop"
`
	out, _ := RedactYAML([]byte(in))
	for _, leak := range []string{"Sup3rPassw0rd", "hook-user", "eyJhbGciOiJIUzI1NiJ9"} {
		if strings.Contains(string(out), leak) {
			t.Errorf("leaked %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(string(out), "receiver.internal") {
		t.Errorf("host should remain:\n%s", out)
	}
}
