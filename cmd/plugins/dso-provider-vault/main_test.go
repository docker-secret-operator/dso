package main

import (
	"strings"
	"testing"
)

// TestValidateVaultSecretName_RejectsMountEscape is a regression test for the
// mount-escape finding: the secret name from dso.yaml was interpolated into
// the Vault API path with no validation, so "../../sys/policy/root" read a
// path outside the configured KV mount (anything the token was authorized
// for), defeating the implicit boundary that `mount` is supposed to provide.
func TestValidateVaultSecretName_RejectsMountEscape(t *testing.T) {
	escapes := []string{
		"../../sys/policy/root",
		"../../../transit/keys/x",
		"app/../../sys/health",
		"..",
		"a/../../b",
		"/absolute/path",
		"",
	}
	for _, name := range escapes {
		if err := validateVaultSecretName(name); err == nil {
			t.Errorf("validateVaultSecretName(%q): expected rejection, got nil", name)
		}
	}
}

// TestValidateVaultSecretName_AllowsLegitimateNames confirms the fix rejects
// traversal specifically, not slashes in general -- nested KV paths are a
// completely normal Vault convention and must keep working.
func TestValidateVaultSecretName_AllowsLegitimateNames(t *testing.T) {
	valid := []string{
		"db_password",
		"app/db/password",
		"team-a/service_b/api.key",
		"deeply/nested/path/to/secret",
		"has..dots.but.not.a.segment",
	}
	for _, name := range valid {
		if err := validateVaultSecretName(name); err != nil {
			t.Errorf("validateVaultSecretName(%q): expected acceptance, got: %v", name, err)
		}
	}
}

// TestRequireSecureVaultAddr covers the cleartext-token finding: the Vault
// token is sent as a request header, so plain HTTP to a remote host exposes
// it on the network path. Loopback stays allowed so the documented default
// and local development keep working.
func TestRequireSecureVaultAddr(t *testing.T) {
	allowed := []string{
		"http://127.0.0.1:8200", // the built-in default
		"http://localhost:8200",
		"http://[::1]:8200",
		"https://vault.internal:8200",
		"https://vault.example.com",
	}
	for _, addr := range allowed {
		if err := requireSecureVaultAddr(addr); err != nil {
			t.Errorf("requireSecureVaultAddr(%q): expected acceptance, got: %v", addr, err)
		}
	}

	rejected := []string{
		"http://vault.internal:8200", // remote over cleartext -- the actual risk
		"http://10.0.0.5:8200",
		"http://vault.example.com",
		"ftp://vault.example.com",
		"tcp://vault:8200",
	}
	for _, addr := range rejected {
		if err := requireSecureVaultAddr(addr); err == nil {
			t.Errorf("requireSecureVaultAddr(%q): expected rejection, got nil", addr)
		}
	}
}

// TestGetSecret_UninitializedClientDoesNotPanic covers the missing nil-client
// guard. ProviderRPCServer exposes GetSecret over RPC with no enforcement
// that Init ran first, and Vault was the only provider without this guard --
// it nil-dereferenced and panicked the plugin process instead of erroring.
func TestGetSecret_UninitializedClientDoesNotPanic(t *testing.T) {
	p := &VaultProvider{} // client == nil, as before Init

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GetSecret panicked on an uninitialized provider: %v", r)
		}
	}()

	_, err := p.GetSecret("db_password")
	if err == nil {
		t.Fatal("expected an error from an uninitialized provider, got nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected an 'not initialized' error, got: %v", err)
	}
}

// TestInit_RejectsCleartextRemoteAddr wires the address check through Init,
// confirming the guard is actually reachable from the real entry point and
// not just unit-testable in isolation.
func TestInit_RejectsCleartextRemoteAddr(t *testing.T) {
	p := &VaultProvider{}
	err := p.Init(map[string]string{
		"address": "http://vault.internal:8200",
		"token":   "hvs.exampletoken",
	})
	if err == nil {
		t.Fatal("expected Init to reject cleartext http to a remote host")
	}
	if !strings.Contains(err.Error(), "cleartext") {
		t.Errorf("expected a cleartext-related error, got: %v", err)
	}
}

// TestInit_RequiresToken preserves the pre-existing contract: a missing token
// is still an error, and the new address validation must not mask it for a
// legitimate (loopback) address.
func TestInit_RequiresToken(t *testing.T) {
	p := &VaultProvider{}
	err := p.Init(map[string]string{"address": "http://127.0.0.1:8200"})
	if err == nil {
		t.Fatal("expected Init to require a token")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("expected a token-related error, got: %v", err)
	}
}
