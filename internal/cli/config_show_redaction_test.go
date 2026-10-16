package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fake credentials: distinct, recognisable, >= 8 chars.
const (
	showHMAC   = "FAKE-show-hmac-secret-0123456789"
	showLegacy = "FAKE-show-legacy-token-abcdef"
	showBcrypt = "$2a$10$FAKEshowBCRYPThashForTestingOnly0123456789abcdefg"
	showAccess = "FAKE-show-access-key-0123456"
	showSecret = "FAKE-show-secret-key-9876543210"
	showSlack  = "T0FAKE/B0FAKE/FAKEshowSLACKtoken123"
)

const showConfigFixture = `agent:
  watch:
    webhook:
      enabled: true
      auth_token: ` + showLegacy + `
      hmac_secret: ` + showHMAC + `
webui:
  enabled: true
  username: operator
  password_hash: ` + showBcrypt + `
providers:
  aws:
    type: aws
    region: eu-west-1
    auth:
      method: access_key
      params:
        access_key: ` + showAccess + `
        secret_key: ` + showSecret + `
notifications:
  enabled: true
  webhooks:
    - url: https://hooks.slack.com/services/` + showSlack + `
secrets:
  - name: db_password
    provider: aws
`

// captureShow runs showConfig and returns what it wrote to stdout and stderr.
func captureShow(t *testing.T, path string) (stdout, stderr string, err error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	err = showConfig(path)
	_ = wo.Close()
	_ = we.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	var bo, be bytes.Buffer
	_, _ = io.Copy(&bo, ro)
	_, _ = io.Copy(&be, re)
	return bo.String(), be.String(), err
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dso.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigShow_NeverPrintsCredentialValues(t *testing.T) {
	out, errOut, err := captureShow(t, writeTemp(t, showConfigFixture))
	if err != nil {
		t.Fatalf("showConfig: %v", err)
	}
	for _, secret := range []string{showHMAC, showLegacy, showBcrypt, showAccess, showSecret, showSlack} {
		if strings.Contains(out, secret) || strings.Contains(errOut, secret) {
			t.Errorf("credential %q appeared in config show output:\nSTDOUT:\n%s\nSTDERR:\n%s", secret, out, errOut)
		}
	}
	// The three fields named in the security review, by key.
	for _, key := range []string{"hmac_secret", "auth_token", "password_hash"} {
		if !strings.Contains(out, key+": \"[REDACTED]\"") {
			t.Errorf("%s not shown as redacted:\n%s", key, out)
		}
	}
}

func TestConfigShow_PreservesNonSensitiveOutputAndHeader(t *testing.T) {
	path := writeTemp(t, showConfigFixture)
	out, errOut, err := captureShow(t, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Config file: " + path, "username: operator", "region: eu-west-1", "method: access_key",
		"name: db_password", "provider: aws", "https://hooks.slack.com/", "enabled: true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "redacted") {
		t.Errorf("expected a note that values are redacted on stderr, got %q", errOut)
	}
}

func TestConfigShow_FailsClosedOnUnparseableConfig(t *testing.T) {
	bad := "agent:\n  watch:\n    webhook:\n      hmac_secret: " + showHMAC + "\n   broken: [unterminated\n"
	out, errOut, err := captureShow(t, writeTemp(t, bad))
	if err == nil {
		t.Fatalf("expected an error for an unparseable config, got output:\n%s", out)
	}
	for _, s := range []string{out, errOut, err.Error()} {
		if strings.Contains(s, showHMAC) {
			t.Errorf("credential leaked on the error path: %q", s)
		}
	}
	if strings.Contains(out, "hmac_secret") {
		t.Errorf("raw file content was printed despite the parse failure:\n%s", out)
	}
}

func TestConfigShow_MissingFileErrorUnchanged(t *testing.T) {
	_, _, err := captureShow(t, filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("missing file error changed: %v", err)
	}
}

// Regression for review finding M0-3: free-form provider settings carried
// credentials under ordinary-looking keys and were printed verbatim.
func TestConfigShow_FreeFormProviderValuesAreRedactedByValue(t *testing.T) {
	const doc = `providers:
  p1:
    type: custom
    config:
      endpoint: "https://admin:hunter2pass@vault.internal:8200"
      connection: "postgres://svc:dbpassword99@db/app"
      region: us-east-1
      role_id: "11111111-2222-3333-4444-555555555555"
      jwt: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijklmnop"
      notes: "ssn 123-45-6789"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "dso.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := captureShow(t, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2pass", "dbpassword99", "11111111-2222-3333-4444-555555555555",
		"eyJhbGciOiJIUzI1NiJ9", "abcdefghijklmnop", "123-45-6789"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Errorf("dso config show printed %q:\n%s", leak, stdout)
		}
	}
	if !strings.Contains(stdout, "region: us-east-1") {
		t.Errorf("benign setting hidden:\n%s", stdout)
	}
}

// Review finding M0-4: an existing /etc/dso/dso.yaml created by an older
// bootstrap stays 0644. `config show` must say so and must not change it.
func TestConfigShow_WarnsAboutPermissiveConfigWithoutChangingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dso.yaml")
	if err := os.WriteFile(path, []byte(showConfigFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := captureShow(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "WARNING") || !strings.Contains(stderr, "0644") || !strings.Contains(stderr, "chmod") {
		t.Errorf("no actionable permission warning on stderr: %q", stderr)
	}
	if strings.Contains(stderr, showHMAC) {
		t.Error("warning leaked a credential")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("config show modified the file mode to %04o", fi.Mode().Perm())
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, _ := captureShow(t, path); strings.Contains(stderr, "WARNING") {
		t.Errorf("0600 config produced a warning: %q", stderr)
	}
}
