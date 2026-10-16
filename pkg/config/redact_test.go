package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Distinct, recognisable fake credentials (all >= 8 chars).
const (
	fakeHMAC     = "FAKE-hmac-secret-0123456789"
	fakeLegacy   = "FAKE-legacy-bearer-token-abcdef"
	fakeBcrypt   = "$2a$10$FAKEbcryptHASHforTESTINGonly0123456789abcdefghijkl"
	fakeAccess   = "FAKE-access-key-ID-0123456"
	fakeSecretK  = "FAKE-secret-access-key-9876543210"
	fakeProvTok  = "FAKE-provider-config-token-zzzz"
	fakeSlackTok = "T0FAKE/B0FAKE/FAKEslackTOKENvalue123"
	fakeURLPass  = "FAKE-url-userinfo-pass-777"
	fakeUnknown  = "FAKE-unknown-key-value-5555"
)

func allFakes() []string {
	return []string{fakeHMAC, fakeLegacy, fakeBcrypt, fakeAccess, fakeSecretK, fakeProvTok, fakeSlackTok, fakeURLPass, fakeUnknown}
}

// realistic holds every credential kind on a single line each, so the
// byte-preserving path is exercised.
const realistic = `# DSO configuration
agent:
  watch:
    webhook:
      enabled: true
      auth_token: ` + fakeLegacy + `   # legacy bearer
      hmac_secret: "` + fakeHMAC + `"
      timestamp_tolerance: 5m
webui:
  enabled: true
  username: operator
  password_hash: '` + fakeBcrypt + `'
  session_idle_timeout: 30m

providers:
  aws:
    type: aws
    region: eu-west-1
    auth:
      method: access_key
      params:
        access_key: ` + fakeAccess + `
        secret_key: "` + fakeSecretK + `"
    config:
      endpoint: https://secretsmanager.eu-west-1.amazonaws.com
      token: ` + fakeProvTok + `
notifications:
  enabled: true
  webhooks:
    - url: https://hooks.slack.com/services/` + fakeSlackTok + `
      timeout: 10s
    - url: https://user:` + fakeURLPass + `@alerts.example.com:8443/hook
secrets:
  - name: db_password
    provider: aws
    mappings:
      DB_PASSWORD: prod/db/password
vendor_extension:
  api_key: ` + fakeUnknown + `
  note: not sensitive
`

func mustRedact(t *testing.T, in string) string {
	t.Helper()
	out, err := RedactYAML([]byte(in))
	if err != nil {
		t.Fatalf("RedactYAML: %v", err)
	}
	return string(out)
}

func assertNoSecrets(t *testing.T, out string, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("output still contains secret %q:\n%s", s, out)
		}
	}
}

func TestRedactYAML_EveryCredentialKindIsRedacted(t *testing.T) {
	out := mustRedact(t, realistic)
	assertNoSecrets(t, out, allFakes()...)
	// The three fields named in the review, explicitly.
	for _, key := range []string{"hmac_secret", "auth_token", "password_hash"} {
		if !strings.Contains(out, key+":") {
			t.Errorf("key %s missing from output (redaction must keep the key)", key)
		}
	}
	if strings.Count(out, RedactedMarker) < 9 {
		t.Errorf("expected at least 9 redactions, got %d:\n%s", strings.Count(out, RedactedMarker), out)
	}
}

func TestRedactYAML_NonSensitiveContentIsPreserved(t *testing.T) {
	out := mustRedact(t, realistic)
	for _, want := range []string{
		"# DSO configuration", "enabled: true", "timestamp_tolerance: 5m", "username: operator",
		"session_idle_timeout: 30m", "region: eu-west-1", "method: access_key",
		"endpoint: https://secretsmanager.eu-west-1.amazonaws.com", "timeout: 10s",
		"https://hooks.slack.com/", "alerts.example.com:8443", // destination host/port stay visible
		"name: db_password", "DB_PASSWORD: prod/db/password", // `secrets:` list and a PATH named *PASSWORD are not credentials
		"note: not sensitive", "# legacy bearer", // comments survive
	} {
		if !strings.Contains(out, want) {
			t.Errorf("non-sensitive content %q lost:\n%s", want, out)
		}
	}
}

// Byte-preserving path: only lines that carry a credential may differ.
func TestRedactYAML_OnlyCredentialLinesChange(t *testing.T) {
	out := mustRedact(t, realistic)
	inLines, outLines := strings.Split(realistic, "\n"), strings.Split(out, "\n")
	if len(inLines) != len(outLines) {
		t.Fatalf("line count changed %d -> %d (in-place edit expected):\n%s", len(inLines), len(outLines), out)
	}
	changed := 0
	for i := range inLines {
		if inLines[i] == outLines[i] {
			continue
		}
		changed++
		hasSecret := false
		for _, s := range allFakes() {
			if strings.Contains(inLines[i], s) {
				hasSecret = true
			}
		}
		if !hasSecret {
			t.Errorf("line %d changed without containing a credential:\n  in:  %q\n  out: %q", i+1, inLines[i], outLines[i])
		}
	}
	if changed != 9 {
		t.Errorf("changed %d lines, want 9 (one per credential)", changed)
	}
}

func TestRedactYAML_OutputIsValidYAMLWithSameStructure(t *testing.T) {
	out := mustRedact(t, realistic)
	var before, after Config
	if err := yaml.Unmarshal([]byte(realistic), &before); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(out), &after); err != nil {
		t.Fatalf("redacted output is not valid YAML: %v\n%s", err, out)
	}
	if after.WebUI.Username != before.WebUI.Username || len(after.Secrets) != len(before.Secrets) ||
		after.Providers["aws"].Region != before.Providers["aws"].Region ||
		len(after.Notifications.Webhooks) != 2 {
		t.Errorf("structure changed by redaction: %+v", after)
	}
	if after.Agent.Watch.Webhook.HMACSecret != RedactedMarker {
		t.Errorf("hmac_secret = %q", after.Agent.Watch.Webhook.HMACSecret)
	}
	if got := after.Notifications.Webhooks[0].URL; !strings.HasPrefix(got, "https://hooks.slack.com/") || strings.Contains(got, "FAKE") {
		t.Errorf("notification URL = %q", got)
	}
	if got := after.Notifications.Webhooks[1].URL; got != "https://alerts.example.com:8443/"+RedactedMarker {
		t.Errorf("userinfo URL = %q", got)
	}
}

func TestRedactYAML_IsIdempotent(t *testing.T) {
	once := mustRedact(t, realistic)
	if twice := mustRedact(t, once); twice != once {
		t.Errorf("second pass changed output:\n%s\n---\n%s", once, twice)
	}
}

func TestRedactYAML_UnsetCredentialsStayVisiblyUnset(t *testing.T) {
	out := mustRedact(t, "agent:\n  watch:\n    webhook:\n      hmac_secret: \"\"\n      auth_token:\n")
	if strings.Contains(out, RedactedMarker) {
		t.Errorf("an empty/unset credential must not be shown as set:\n%s", out)
	}
}

// Shapes where a byte-preserving edit is not provably safe fall back to
// re-encoding the tree; the credential must still be gone.
func TestRedactYAML_FallbackShapesStillRedact(t *testing.T) {
	cases := map[string]string{
		"flow mapping":        "agent: {watch: {webhook: {hmac_secret: " + fakeHMAC + ", enabled: true}}}\n",
		"block literal":       "agent:\n  watch:\n    webhook:\n      hmac_secret: |\n        " + fakeHMAC + "\n      enabled: true\n",
		"block folded":        "webui:\n  password_hash: >\n    " + fakeBcrypt + "\n  enabled: true\n",
		"anchor + alias":      "base: &k " + fakeHMAC + "\nagent:\n  watch:\n    webhook:\n      hmac_secret: *k\n",
		"anchored credential": "agent:\n  watch:\n    webhook:\n      hmac_secret: &k " + fakeHMAC + "\n      auth_token: *k\n",
		"multi-document":      "webui:\n  password_hash: " + fakeBcrypt + "\n---\nagent:\n  watch:\n    webhook:\n      hmac_secret: " + fakeHMAC + "\n",
		"quote with escapes":  "agent:\n  watch:\n    webhook:\n      hmac_secret: \"" + fakeHMAC + "\\\"tail\\n\"\n",
		"single quote escape": "agent:\n  watch:\n    webhook:\n      hmac_secret: '" + fakeHMAC + "''tail'\n",
		"tagged":              "agent:\n  watch:\n    webhook:\n      hmac_secret: !!str " + fakeHMAC + "\n",
		"multi-line plain":    "agent:\n  watch:\n    webhook:\n      hmac_secret: " + fakeHMAC + "\n        continued-secret-words-here\n",
	}
	for name, in := range cases {
		out, err := RedactYAML([]byte(in))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		assertNoSecrets(t, string(out), fakeHMAC, fakeBcrypt, "continued-secret-words-here")
		if !strings.Contains(string(out), RedactedMarker) {
			t.Errorf("%s: nothing was redacted:\n%s", name, out)
		}
		var any interface{}
		if err := yaml.Unmarshal(out, &any); err != nil {
			t.Errorf("%s: output is not valid YAML: %v\n%s", name, err, out)
		}
	}
}

// A secret repeated somewhere the schema does not point at (a comment, an
// unrelated key) is still scrubbed.
func TestRedactYAML_ScrubsRepeatedCopiesOfARedactedValue(t *testing.T) {
	in := "# old value was " + fakeHMAC + "\nagent:\n  watch:\n    webhook:\n      hmac_secret: " + fakeHMAC + "\nnotes: " + fakeHMAC + "\n"
	assertNoSecrets(t, mustRedact(t, in), fakeHMAC)
}

// Keys the schema does not define are covered by the name-based backstop
// (future fields, typos, vendor extensions) -- fail closed.
func TestRedactYAML_UnknownCredentialLookingKeysAreRedacted(t *testing.T) {
	in := "hmac_secrt: " + fakeUnknown + "\nextra:\n  nested:\n    client_secret: " + fakeAccess + "\n    Password: " + fakeSecretK + "\n    harmless: visible\n"
	out := mustRedact(t, in)
	assertNoSecrets(t, out, fakeUnknown, fakeAccess, fakeSecretK)
	if !strings.Contains(out, "harmless: visible") {
		t.Errorf("non-credential unknown key was touched:\n%s", out)
	}
}

// provider.config redacts by KEY; the other entries (endpoint, address) stay.
func TestRedactYAML_ProviderConfigRedactsByKey_AndAuthParamsRedactsEverything(t *testing.T) {
	in := "providers:\n  v:\n    type: vault\n    config:\n      address: https://vault.example.com:8200\n      token: " + fakeProvTok + "\n      namespace: team-a\n    auth:\n      params:\n        role_id: " + fakeAccess + "\n        anything_at_all: " + fakeSecretK + "\n"
	out := mustRedact(t, in)
	assertNoSecrets(t, out, fakeProvTok, fakeAccess, fakeSecretK)
	for _, keep := range []string{"address: https://vault.example.com:8200", "namespace: team-a", "role_id:", "anything_at_all:"} {
		if !strings.Contains(out, keep) {
			t.Errorf("%q missing:\n%s", keep, out)
		}
	}
}

func TestRedactYAML_FailsClosedOnInvalidYAML(t *testing.T) {
	in := "agent:\n  watch:\n    webhook:\n      hmac_secret: " + fakeHMAC + "\n   bad indent: [unterminated\n"
	out, err := RedactYAML([]byte(in))
	if err == nil {
		t.Fatalf("expected an error for invalid YAML, got output:\n%s", out)
	}
	if out != nil {
		t.Errorf("must not return any bytes on error, got %q", out)
	}
	if strings.Contains(err.Error(), fakeHMAC) {
		t.Errorf("error message leaks the credential: %v", err)
	}
}

func TestRedactYAML_EmptyAndCredentialFreeInputPassThrough(t *testing.T) {
	for _, in := range []string{"", "\n", "# only a comment\n", "agent:\n  watch:\n    webhook:\n      enabled: false\n"} {
		if out := mustRedact(t, in); out != in {
			t.Errorf("credential-free input changed:\n in: %q\nout: %q", in, out)
		}
	}
}

// ---- guard: a credential field added to the schema without a tag fails here ----

func TestSchemaCredentialFieldsAreTagged(t *testing.T) {
	// Fields that look credential-like by name but are NOT credentials.
	notCredentials := map[string]string{
		"secrets": "list of secret-mapping definitions (names, providers); no values",
	}
	// Fields that are credential-bearing but do not match the name pattern.
	mustBeTagged := map[string]bool{"url": true, "params": true, "config": true}

	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.PkgPath != "" {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			if name == "" || name == "-" {
				continue
			}
			tag := f.Tag.Get("dso")
			if tag != "" {
				if _, ok := parseDSOTag(tag); !ok {
					t.Errorf("%s.%s has unknown dso tag %q", path, f.Name, tag)
				}
			}
			looksSecret := credentialKeyPattern.MatchString(name)
			if _, ok := notCredentials[name]; ok {
				looksSecret = false
			}
			ft := f.Type
			isLeafish := ft.Kind() == reflect.String || ft.Kind() == reflect.Map
			if looksSecret && tag == "" {
				t.Errorf("%s.%s (yaml %q) looks like a credential but has no `dso` tag", path, f.Name, name)
			}
			if mustBeTagged[name] && isLeafish && tag == "" {
				t.Errorf("%s.%s (yaml %q) carries credentials but has no `dso` tag", path, f.Name, name)
			}
			walk(f.Type, path+"."+f.Name)
		}
	}
	walk(reflect.TypeOf(Config{}), "Config")
}

func TestSchemaTagsCoverTheFieldsNamedInTheSecurityReview(t *testing.T) {
	for _, tc := range []struct {
		typ   reflect.Type
		field string
		kind  redactKind
	}{
		{reflect.TypeOf(WebhookConfig{}), "HMACSecret", kindSecret},
		{reflect.TypeOf(WebhookConfig{}), "AuthToken", kindSecret},
		{reflect.TypeOf(WebUIConfig{}), "PasswordHash", kindSecret},
		{reflect.TypeOf(AuthConfig{}), "Params", kindSecret},
		{reflect.TypeOf(NotificationWebhook{}), "URL", kindURL},
		{reflect.TypeOf(ProviderConfig{}), "Config", kindSecretByKey},
	} {
		f, ok := tc.typ.FieldByName(tc.field)
		if !ok {
			t.Fatalf("%s.%s not found", tc.typ, tc.field)
		}
		got, ok := parseDSOTag(f.Tag.Get("dso"))
		if !ok || got != tc.kind {
			t.Errorf("%s.%s dso tag = %q, want kind %v", tc.typ.Name(), tc.field, f.Tag.Get("dso"), tc.kind)
		}
	}
}

// "Already redacted" is an EXACT match. A real credential that merely contains
// the marker text must not slip through.
func TestRedactYAML_CredentialContainingMarkerTextIsStillRedacted(t *testing.T) {
	const sneaky = "prefix[REDACTED]actual-secret-tail-12345"
	out := mustRedact(t, "agent:\n  watch:\n    webhook:\n      hmac_secret: \""+sneaky+"\"\n")
	assertNoSecrets(t, out, "actual-secret-tail-12345", sneaky)
}
