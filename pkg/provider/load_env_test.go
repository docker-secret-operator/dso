package provider

import (
	"strings"
	"testing"
)

// envHas reports whether the "KEY=VALUE" slice contains key, and its value.
func envHas(env []string, key string) (string, bool) {
	for _, kv := range env {
		if name, val, ok := strings.Cut(kv, "="); ok && name == key {
			return val, true
		}
	}
	return "", false
}

// TestSanitizeEnv_AlwaysSetsFixedPath preserves the original guarantee: the
// plugin gets a fixed, known-good PATH rather than inheriting the daemon's.
func TestSanitizeEnv_AlwaysSetsFixedPath(t *testing.T) {
	t.Setenv("PATH", "/attacker/controlled/bin")

	for _, prov := range []string{"aws", "azure", "huawei", "vault", "unknown"} {
		env := sanitizeEnv(prov)
		got, ok := envHas(env, "PATH")
		if !ok {
			t.Fatalf("sanitizeEnv(%q): PATH missing", prov)
		}
		if got != "/usr/local/bin:/usr/bin:/bin" {
			t.Errorf("sanitizeEnv(%q): PATH = %q, want the fixed default (must not inherit the daemon's PATH)", prov, got)
		}
	}
}

// TestSanitizeEnv_PassesProviderCredentials is a regression test for the
// finding that every documented env-var credential path was silently broken:
// sanitizeEnv returned only PATH, so AWS_ACCESS_KEY_ID, AZURE_CLIENT_SECRET,
// and the HUAWEI_* vars the Huawei plugin reads via os.Getenv were always
// empty inside the plugin process.
func TestSanitizeEnv_PassesProviderCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "aws-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("AZURE_CLIENT_SECRET", "azure-secret")
	t.Setenv("HUAWEI_ACCESS_KEY", "huawei-key")
	t.Setenv("HUAWEI_SECURITY_TOKEN", "huawei-token")
	t.Setenv("VAULT_TOKEN", "vault-token")

	cases := []struct {
		provider string
		key      string
		want     string
	}{
		{"aws", "AWS_ACCESS_KEY_ID", "aws-key"},
		{"aws", "AWS_SECRET_ACCESS_KEY", "aws-secret"},
		{"azure", "AZURE_CLIENT_SECRET", "azure-secret"},
		{"huawei", "HUAWEI_ACCESS_KEY", "huawei-key"},
		{"huawei", "HUAWEI_SECURITY_TOKEN", "huawei-token"},
		{"vault", "VAULT_TOKEN", "vault-token"},
	}
	for _, c := range cases {
		got, ok := envHas(sanitizeEnv(c.provider), c.key)
		if !ok {
			t.Errorf("sanitizeEnv(%q): %s not passed through — documented credential path is broken", c.provider, c.key)
			continue
		}
		if got != c.want {
			t.Errorf("sanitizeEnv(%q): %s = %q, want %q", c.provider, c.key, got, c.want)
		}
	}
}

// TestSanitizeEnv_IsPerProviderLeastPrivilege confirms the allow-list is
// scoped per provider: the AWS plugin has no reason to see Azure or Vault
// credentials, so a compromised plugin cannot harvest another backend's keys.
func TestSanitizeEnv_IsPerProviderLeastPrivilege(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("AZURE_CLIENT_SECRET", "azure-secret")
	t.Setenv("HUAWEI_SECRET_KEY", "huawei-secret")
	t.Setenv("VAULT_TOKEN", "vault-token")

	leaks := []struct {
		provider string
		mustNot  []string
	}{
		{"aws", []string{"AZURE_CLIENT_SECRET", "HUAWEI_SECRET_KEY", "VAULT_TOKEN"}},
		{"azure", []string{"AWS_SECRET_ACCESS_KEY", "HUAWEI_SECRET_KEY", "VAULT_TOKEN"}},
		{"huawei", []string{"AWS_SECRET_ACCESS_KEY", "AZURE_CLIENT_SECRET", "VAULT_TOKEN"}},
		{"vault", []string{"AWS_SECRET_ACCESS_KEY", "AZURE_CLIENT_SECRET", "HUAWEI_SECRET_KEY"}},
	}
	for _, l := range leaks {
		env := sanitizeEnv(l.provider)
		for _, key := range l.mustNot {
			if _, ok := envHas(env, key); ok {
				t.Errorf("sanitizeEnv(%q) leaked %s — the allow-list must be per-provider", l.provider, key)
			}
		}
	}
}

// TestSanitizeEnv_DoesNotLeakUnrelatedDaemonEnv is the core security property:
// widening the allow-list must not turn into wholesale inheritance of the
// daemon's environment, which would expose DSO's own master key and any other
// host secret to the plugin subprocess.
func TestSanitizeEnv_DoesNotLeakUnrelatedDaemonEnv(t *testing.T) {
	t.Setenv("DSO_MASTER_KEY", "super-secret-master-key")
	t.Setenv("SOME_OTHER_APP_TOKEN", "unrelated-token")
	t.Setenv("DATABASE_PASSWORD", "db-password")

	for _, prov := range []string{"aws", "azure", "huawei", "vault", "unknown"} {
		env := sanitizeEnv(prov)
		for _, forbidden := range []string{"DSO_MASTER_KEY", "SOME_OTHER_APP_TOKEN", "DATABASE_PASSWORD"} {
			if _, ok := envHas(env, forbidden); ok {
				t.Errorf("sanitizeEnv(%q) leaked unrelated daemon variable %s", prov, forbidden)
			}
		}
	}
}

// TestSanitizeEnv_OmitsUnsetVariables confirms allow-listed-but-unset
// variables are not passed as empty strings, which would otherwise shadow a
// credential the plugin's own SDK chain could have resolved another way (e.g.
// an empty AWS_PROFILE defeating instance-metadata fallback).
func TestSanitizeEnv_OmitsUnsetVariables(t *testing.T) {
	// Deliberately not set: AWS_PROFILE.
	for _, kv := range sanitizeEnv("aws") {
		if strings.HasPrefix(kv, "AWS_PROFILE=") {
			t.Errorf("unset AWS_PROFILE was passed through as %q; unset vars must be omitted entirely", kv)
		}
	}
}

// TestSanitizeEnv_PassesCommonVars covers HOME (needed for
// ~/.aws/credentials and az login caches) and proxy/TLS settings, which apply
// to every provider.
func TestSanitizeEnv_PassesCommonVars(t *testing.T) {
	t.Setenv("HOME", "/home/dso")
	t.Setenv("HTTPS_PROXY", "http://egress-proxy:3128")
	t.Setenv("SSL_CERT_FILE", "/etc/ssl/private-ca.pem")

	env := sanitizeEnv("aws")
	for key, want := range map[string]string{
		"HOME":          "/home/dso",
		"HTTPS_PROXY":   "http://egress-proxy:3128",
		"SSL_CERT_FILE": "/etc/ssl/private-ca.pem",
	} {
		got, ok := envHas(env, key)
		if !ok {
			t.Errorf("%s not passed through to plugin", key)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// TestSanitizeEnv_UnknownProviderGetsNoCredentials ensures an unrecognized
// provider name falls back to the common set only, rather than defaulting to
// something permissive.
func TestSanitizeEnv_UnknownProviderGetsNoCredentials(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")

	env := sanitizeEnv("some-third-party-provider")
	if _, ok := envHas(env, "AWS_SECRET_ACCESS_KEY"); ok {
		t.Error("unknown provider received AWS credentials")
	}
	if _, ok := envHas(env, "PATH"); !ok {
		t.Error("unknown provider should still get a PATH")
	}
}
