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
	key1 := DeriveKeyFromPassword(password, salt)
	key2 := DeriveKeyFromPassword(password, salt)

	if len(key1) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(key1))
	}

	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatal("deterministic key derivation failed")
		}
	}
}

func TestDeriveKeyFromPassword_DefaultSalt(t *testing.T) {
	key := DeriveKeyFromPassword("pass", nil)
	if len(key) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(key))
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
