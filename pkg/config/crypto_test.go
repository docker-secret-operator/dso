package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestCryptoManager(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	cm, err := NewCryptoManager(key)
	if err != nil {
		t.Fatalf("failed to create crypto manager: %v", err)
	}

	plaintext := "secret message"
	ciphertext, err := cm.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if ciphertext == plaintext {
		t.Error("ciphertext should be different from plaintext")
	}

	decrypted, err := cm.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("decrypted mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestNewCryptoManager_InvalidKey(t *testing.T) {
	_, err := NewCryptoManager([]byte("short"))
	if err == nil {
		t.Error("expected error for short key")
	}
}

func TestCryptoManager_DecryptError(t *testing.T) {
	key := make([]byte, 32)
	cm, _ := NewCryptoManager(key)
	_, err := cm.Decrypt("not-base64!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}

	_, err = cm.Decrypt(base64.StdEncoding.EncodeToString([]byte("short")))
	if err == nil {
		t.Error("expected error for short ciphertext")
	}
}

func TestDeriveKeyFromPassword(t *testing.T) {
	password := "mypassword"
	salt := make([]byte, 16)
	key1, err := DeriveKeyFromPassword(password, salt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	key2, err := DeriveKeyFromPassword(password, salt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(key1) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(key1))
	}

	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatal("deterministic key derivation failed")
		}
	}
}

// SEC-H4: an invalid salt must produce an error, never a silently substituted
// static salt.
func TestDeriveKeyFromPassword_InvalidSalt(t *testing.T) {
	if _, err := DeriveKeyFromPassword("pass", nil); err == nil {
		t.Error("expected error for nil salt, got nil")
	}
	if _, err := DeriveKeyFromPassword("pass", make([]byte, 8)); err == nil {
		t.Error("expected error for short salt, got nil")
	}
	if _, err := DeriveKeyFromPassword("pass", make([]byte, 32)); err == nil {
		t.Error("expected error for oversized salt, got nil")
	}
}

// Distinct salts must yield distinct keys (defeats precomputed-table attacks).
func TestDeriveKeyFromPassword_SaltUniqueness(t *testing.T) {
	salt1 := make([]byte, 16)
	salt2 := make([]byte, 16)
	salt2[0] = 1
	k1, err := DeriveKeyFromPassword("pass", salt1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k2, err := DeriveKeyFromPassword("pass", salt2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(k1) == string(k2) {
		t.Fatal("different salts produced identical keys")
	}
}

func TestEncryptDecryptProviderConfig(t *testing.T) {
	key := make([]byte, 32)
	cm, _ := NewCryptoManager(key)

	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"test": {
				Type: "vault",
				Config: map[string]string{
					"token": "my-token",
				},
				Auth: AuthConfig{
					Params: map[string]string{
						"secret": "my-secret",
					},
				},
			},
		},
	}

	err := cm.EncryptProviderConfig(cfg)
	if err != nil {
		t.Fatalf("EncryptProviderConfig failed: %v", err)
	}

	prov := cfg.Providers["test"]
	if prov.Config["token"] == "my-token" {
		t.Error("Token should be encrypted")
	}
	if prov.Auth.Params["secret"] == "my-secret" {
		t.Error("Auth param should be encrypted")
	}

	err = cm.DecryptProviderConfig(cfg)
	if err != nil {
		t.Fatalf("DecryptProviderConfig failed: %v", err)
	}

	prov = cfg.Providers["test"]
	if prov.Config["token"] != "my-token" {
		t.Errorf("decrypted token mismatch: got %q", prov.Config["token"])
	}
	if prov.Auth.Params["secret"] != "my-secret" {
		t.Errorf("decrypted auth param mismatch: got %q", prov.Auth.Params["secret"])
	}
}

func TestLoadConfigWithDecryption(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Create an encrypted config string
	cm, _ := NewCryptoManager(key)
	tokenEnc, _ := cm.Encrypt("secret-token")

	configData := `
providers:
  vault:
    type: vault
    config:
      token: enc:` + tokenEnc + `
agent:
  cache: true
secrets:
  - name: my-secret
    inject:
      type: env
    mappings:
      KEY: VALUE
`

	// Write to temp file
	tmpDir, _ := os.MkdirTemp(".", "dso-test-")
	defer os.RemoveAll(tmpDir)
	cfgPath := filepath.Join(tmpDir, "dso.yaml")
	_ = os.WriteFile(cfgPath, []byte(configData), 0600)

	// Load it
	cfg, err := LoadConfigWithDecryption(cfgPath, key)
	if err != nil {
		t.Fatalf("LoadConfigWithDecryption failed: %v", err)
	}

	if cfg.Providers["vault"].Config["token"] != "secret-token" {
		t.Errorf("decryption failed during load, got: %s", cfg.Providers["vault"].Config["token"])
	}

	// Test with empty key
	cfg2, err := LoadConfigWithDecryption(cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Providers["vault"].Config["token"] == "secret-token" {
		t.Error("expected encrypted token when no key provided")
	}
}

// TestDecryptProviderConfig_WriteBack verifies the C1 fix: DecryptProviderConfig
// must write updated values back into the Providers map so the caller sees the
// plaintext, not the encrypted ciphertext.
func TestDecryptProviderConfig_WriteBack(t *testing.T) {
	key := make([]byte, 32)
	cm, err := NewCryptoManager(key)
	if err != nil {
		t.Fatalf("NewCryptoManager: %v", err)
	}

	const want = "plaintext-value"
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"p1": {
				Type: "vault",
				Config: map[string]string{"token": want},
				Auth:   AuthConfig{Params: map[string]string{"pass": want}},
			},
			"p2": {
				Type:   "aws",
				Config: map[string]string{"key": want},
			},
		},
	}

	if err := cm.EncryptProviderConfig(cfg); err != nil {
		t.Fatalf("EncryptProviderConfig: %v", err)
	}
	if err := cm.DecryptProviderConfig(cfg); err != nil {
		t.Fatalf("DecryptProviderConfig: %v", err)
	}

	// Both providers must be decrypted in-place in the map.
	for name, prov := range cfg.Providers {
		for k, v := range prov.Config {
			if v != want {
				t.Errorf("provider %q Config[%q] = %q after decrypt, want %q (write-back missing)", name, k, v, want)
			}
		}
		for k, v := range prov.Auth.Params {
			if v != want {
				t.Errorf("provider %q Auth.Params[%q] = %q after decrypt, want %q (write-back missing)", name, k, v, want)
			}
		}
	}
}
