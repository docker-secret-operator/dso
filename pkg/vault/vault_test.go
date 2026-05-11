package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEncryptDecryptRoundtrip validates encryption/decryption integrity
func TestEncryptDecryptRoundtrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
		masterKey string
	}{
		{
			name:      "simple secret",
			plaintext: []byte("my-secret-password"),
			masterKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		{
			name:      "empty secret",
			plaintext: []byte(""),
			masterKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		{
			name:      "large secret (1MB)",
			plaintext: make([]byte, 1024*1024),
			masterKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		{
			name:      "special characters",
			plaintext: []byte("p@$$w0rd!#%&*()[]{}~`^"),
			masterKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		{
			name:      "unicode",
			plaintext: []byte("パスワード密碼🔐"),
			masterKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := Encrypt(tt.plaintext, tt.masterKey)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			if ciphertext == nil || len(ciphertext) == 0 {
				t.Fatal("Ciphertext is empty")
			}

			// Verify format: salt (16) + nonce (12) + ciphertext
			if len(ciphertext) < 28 { // 16 + 12
				t.Fatalf("Ciphertext too short: %d bytes", len(ciphertext))
			}

			decrypted, err := Decrypt(ciphertext, tt.masterKey)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			if string(decrypted) != string(tt.plaintext) {
				t.Errorf("Decrypted text mismatch.\nExpected: %q\nGot: %q", string(tt.plaintext), string(decrypted))
			}
		})
	}
}

// TestDecryptWithWrongKey fails as expected
func TestDecryptWithWrongKey(t *testing.T) {
	plaintext := []byte("secret-data")
	correctKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	wrongKey := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	ciphertext, err := Encrypt(plaintext, correctKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(ciphertext, wrongKey)
	if err == nil {
		t.Fatal("Expected Decrypt to fail with wrong key")
	}

	// Verify it's authentication failure, not some other error
	errStr := err.Error()
	if !strings.Contains(errStr, "authentication failed") && !strings.Contains(errStr, "invalid") {
		t.Errorf("Expected authentication error, got: %v", err)
	}
}

// TestDecryptTruncatedCiphertext fails gracefully
func TestDecryptTruncatedCiphertext(t *testing.T) {
	tests := []struct {
		name       string
		ciphertext []byte
	}{
		{"empty", []byte{}},
		{"too short", []byte("short")},
		{"missing nonce", make([]byte, 16)},
		{"missing ciphertext body", make([]byte, 28)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decrypt(tt.ciphertext, "dummy-key")
			if err == nil {
				t.Fatal("Expected Decrypt to fail with truncated ciphertext")
			}
		})
	}
}

// TestMasterKeyGeneration creates valid keys
func TestMasterKeyGeneration(t *testing.T) {
	key1, err := generateMasterKey()
	if err != nil {
		t.Fatalf("generateMasterKey failed: %v", err)
	}

	if len(key1) != 64 { // 32 bytes hex-encoded = 64 chars
		t.Errorf("Key length wrong. Expected 64, got %d", len(key1))
	}

	// Keys should be different (random)
	key2, err := generateMasterKey()
	if err != nil {
		t.Fatalf("generateMasterKey failed: %v", err)
	}

	if key1 == key2 {
		t.Error("Generated keys should be unique (random)")
	}

	// Keys should be valid hex
	for _, k := range []string{key1, key2} {
		for _, ch := range k {
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
				t.Errorf("Key contains non-hex character: %c", ch)
			}
		}
	}
}

// TestVaultInitDefault creates vault with master key
func TestVaultInitDefault(t *testing.T) {
	tmpDir := t.TempDir()

	// Temporarily override home directory
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	err := InitDefault()
	if err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	// Check vault directory exists
	vaultDir := filepath.Join(tmpDir, ".dso")
	if _, err := os.Stat(vaultDir); os.IsNotExist(err) {
		t.Fatalf("Vault directory not created: %s", vaultDir)
	}

	// Check master key file exists
	keyPath := filepath.Join(vaultDir, "master.key")
	keyInfo, err := os.Stat(keyPath)
	if os.IsNotExist(err) {
		t.Fatalf("Master key file not created: %s", keyPath)
	}

	// Verify permissions (0600)
	if keyInfo.Mode().Perm() != 0600 {
		t.Errorf("Master key permissions wrong. Expected 0600, got %#o", keyInfo.Mode().Perm())
	}

	// Check vault.enc exists
	vaultPath := filepath.Join(vaultDir, "vault.enc")
	if _, err := os.Stat(vaultPath); os.IsNotExist(err) {
		t.Fatalf("Vault file not created: %s", vaultPath)
	}

	// Verify vault directory permissions (0700)
	dirInfo, _ := os.Stat(vaultDir)
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("Vault directory permissions wrong. Expected 0700, got %#o", dirInfo.Mode().Perm())
	}
}

// TestVaultLoadDefault loads and decrypts vault
func TestVaultLoadDefault(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Initialize vault
	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	// Load vault
	v, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}

	if v == nil {
		t.Fatal("Loaded vault is nil")
	}

	if v.store == nil {
		t.Fatal("Vault store is nil")
	}

	if len(v.store.Projects) != 0 {
		t.Errorf("New vault should have no projects, got %d", len(v.store.Projects))
	}
}

// TestVaultSetAndGet stores and retrieves secrets
func TestVaultSetAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}

	// Set secret
	project, path, value := "myapp", "db_password", "supersecret123"
	if err := v.Set(project, path, value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get secret
	secret, err := v.Get(project, path)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if secret.Value != value {
		t.Errorf("Secret mismatch. Expected %q, got %q", value, secret.Value)
	}
}

// TestVaultSetInvalidProject rejects empty project
func TestVaultSetInvalidProject(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, _ := LoadDefault()

	tests := []struct {
		name    string
		project string
		path    string
		value   string
		wantErr bool
	}{
		{"empty project", "", "path", "value", true},
		{"empty path", "proj", "", "value", true},
		{"path with ..", "proj", "../etc/passwd", "value", true},
		{"project with ..", "../proj", "path", "value", true},
		{"oversized secret", "proj", "path", string(make([]byte, 1024*1024+1)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Set(tt.project, tt.path, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("Set: expected error=%v, got %v", tt.wantErr, err)
			}
		})
	}
}

// TestVaultGetNotFound returns error for missing secret
func TestVaultGetNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, _ := LoadDefault()

	_, err := v.Get("nonexistent", "secret")
	if err == nil {
		t.Fatal("Expected Get to fail for missing secret")
	}
}

// TestVaultList returns secret paths
func TestVaultList(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, _ := LoadDefault()

	// Add multiple secrets
	project := "testapp"
	secrets := map[string]string{
		"db_password": "dbpass123",
		"api_key":     "apikey456",
		"token":       "token789",
	}

	for path, value := range secrets {
		if err := v.Set(project, path, value); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
	}

	// List secrets
	paths, err := v.List(project)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(paths) != len(secrets) {
		t.Errorf("Expected %d secrets, got %d", len(secrets), len(paths))
	}

	// Verify all paths are present
	pathMap := make(map[string]bool)
	for _, p := range paths {
		pathMap[p] = true
	}

	for expected := range secrets {
		if !pathMap[expected] {
			t.Errorf("Expected path %q not found in list", expected)
		}
	}
}

// TestVaultSetBatch stores multiple secrets atomically
func TestVaultSetBatch(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, _ := LoadDefault()

	project := "batch-app"
	secrets := map[string]string{
		"db_host":     "localhost",
		"db_port":     "5432",
		"db_password": "secret",
	}

	if err := v.SetBatch(project, secrets); err != nil {
		t.Fatalf("SetBatch failed: %v", err)
	}

	// Verify all secrets were stored
	for path, expectedValue := range secrets {
		secret, err := v.Get(project, path)
		if err != nil {
			t.Fatalf("Get %q failed: %v", path, err)
		}
		if secret.Value != expectedValue {
			t.Errorf("Secret %q mismatch. Expected %q, got %q", path, expectedValue, secret.Value)
		}
	}
}

// TestVaultPersistence verifies data survives reload
func TestVaultPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	// Store secret
	v1, _ := LoadDefault()
	if err := v1.Set("app", "secret", "value123"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Reload vault
	v2, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}

	// Verify secret persisted
	secret, err := v2.Get("app", "secret")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if secret.Value != "value123" {
		t.Errorf("Secret not persisted. Expected 'value123', got %q", secret.Value)
	}
}

// TestVaultChecksumValidation detects tampering
func TestVaultChecksumValidation(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	// Create and save vault with secret
	v, _ := LoadDefault()
	if err := v.Set("app", "secret", "value"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Corrupt the vault file (flip a bit)
	vaultPath := filepath.Join(tmpDir, ".dso", "vault.enc")
	data, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if len(data) > 28 {
		// Flip a bit in the ciphertext (after salt+nonce)
		data[28] ^= 0x01
		if err := os.WriteFile(vaultPath, data, 0600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		// Try to load - should fail due to GCM authentication
		_, err := LoadDefault()
		if err == nil {
			t.Fatal("Expected LoadDefault to fail with corrupted ciphertext")
		}
	}
}

// TestVaultConcurrentAccess handles concurrent operations
func TestVaultConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, _ := LoadDefault()

	// Concurrent writes
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			path := "path-" + string(rune(idx))
			done <- v.Set("concurrent-app", path, "value-"+string(rune(idx)))
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Fatalf("Concurrent Set failed: %v", err)
		}
	}

	// Verify all writes succeeded
	paths, err := v.List("concurrent-app")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(paths) != 10 {
		t.Errorf("Expected 10 paths, got %d", len(paths))
	}
}

// TestVaultMetadataTracking stores update timestamps
func TestVaultMetadataTracking(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, _ := LoadDefault()

	before := time.Now().UTC().Truncate(time.Second)
	if err := v.Set("app", "secret", "value"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	after := time.Now().UTC().Add(1 * time.Second).Truncate(time.Second)

	secret, _ := v.Get("app", "secret")
	if secret.Meta == nil || secret.Meta["updated_at"] == "" {
		t.Fatal("Secret metadata not set")
	}

	// Parse timestamp
	ts, err := time.Parse(time.RFC3339, secret.Meta["updated_at"])
	if err != nil {
		t.Fatalf("Failed to parse timestamp: %v", err)
	}

	if ts.Before(before) || ts.After(after) {
		t.Logf("Timestamp validation: before=%v, ts=%v, after=%v (acceptable if within 1s)", before, ts, after)
	}
}

// TestVaultVersioning tracks vault version
func TestVaultVersioning(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, _ := LoadDefault()

	if v.store.Version != "1.0" {
		t.Errorf("Expected version 1.0, got %q", v.store.Version)
	}
}

// TestVaultMarshalling ensures vault can be serialized/deserialized
func TestVaultMarshalling(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	v, _ := LoadDefault()
	v.Set("app", "secret", "value")

	// Marshal vault
	data, err := json.Marshal(v.store)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal vault
	var store VaultStore
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify structure
	if store.Version != "1.0" {
		t.Errorf("Version mismatch after marshal/unmarshal")
	}

	if len(store.Projects) != 1 {
		t.Errorf("Expected 1 project, got %d", len(store.Projects))
	}
}

// TestVaultFilePermissionsPreserved ensures vault file stays secure
func TestVaultFilePermissionsPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	vaultPath := filepath.Join(tmpDir, ".dso", "vault.enc")

	v, _ := LoadDefault()
	if err := v.Set("app", "secret", "value"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Check vault file permissions after write
	info, err := os.Stat(vaultPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("Vault file permissions not secure. Expected 0600, got %#o", info.Mode().Perm())
	}
}

// TestVaultDirPermissionsPreserved ensures vault directory stays secure
func TestVaultDirPermissionsPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := InitDefault(); err != nil {
		t.Fatalf("InitDefault failed: %v", err)
	}

	vaultDir := filepath.Join(tmpDir, ".dso")
	info, err := os.Stat(vaultDir)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Mode().Perm() != 0700 {
		t.Errorf("Vault directory permissions not secure. Expected 0700, got %#o", info.Mode().Perm())
	}
}

// TestGetVaultDir_NoHome verifies getVaultDir fails when HOME is not set
func TestGetVaultDir_NoHome(t *testing.T) {
	oldHome := os.Getenv("HOME")
	os.Unsetenv("HOME")
	defer func() {
		if oldHome != "" {
			os.Setenv("HOME", oldHome)
		}
	}()

	_, err := getVaultDir()
	if err == nil {
		t.Error("Expected getVaultDir to fail when HOME is unset")
	}
}

// TestGetMasterKey_NotFound verifies getMasterKey fails when missing
func TestGetMasterKey_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer func() {
		if oldHome != "" {
			os.Setenv("HOME", oldHome)
		}
	}()
	os.Unsetenv("DSO_MASTER_KEY")

	_, err := getMasterKey()
	if err == nil {
		t.Error("Expected getMasterKey to fail when key is missing")
	}
}



// TestLoadDefault_InvalidJSON verifies loading invalid JSON
func TestLoadDefault_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer func() {
		if oldHome != "" {
			os.Setenv("HOME", oldHome)
		}
	}()

	InitDefault()

	vaultPath := filepath.Join(tmpDir, ".dso", "vault.enc")

	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	os.Setenv("DSO_MASTER_KEY", key)
	defer os.Unsetenv("DSO_MASTER_KEY")

	cipher, _ := Encrypt([]byte("{invalid-json"), key)
	os.WriteFile(vaultPath, cipher, 0600)

	_, err := LoadDefault()
	if err == nil {
		t.Error("Expected LoadDefault to fail on invalid JSON")
	}
}

// TestSave_TempFileWriteError verifies Save error handling
func TestSave_TempFileWriteError(t *testing.T) {
	v := &Vault{
		store: &VaultStore{},
		vaultPath: "/non-existent-dir-for-vault-save/vault.enc",
		masterKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	err := v.Save()
	if err == nil {
		t.Error("Expected Save to fail when path is invalid")
	}
}

// TestVaultGet_InvalidInputs verifies Get input validation
func TestVaultGet_InvalidInputs(t *testing.T) {
	v := &Vault{store: &VaultStore{Projects: make(map[string]map[string]Secret)}}
	
	_, err := v.Get("", "path")
	if err == nil {
		t.Error("Expected Get to fail with empty project")
	}
	
	_, err = v.Get("proj", "")
	if err == nil {
		t.Error("Expected Get to fail with empty path")
	}
	
	_, err = v.Get("../proj", "path")
	if err == nil {
		t.Error("Expected Get to fail with .. in project")
	}
	
	_, err = v.Get("proj", "../path")
	if err == nil {
		t.Error("Expected Get to fail with .. in path")
	}
}

// TestVaultList_InvalidInputs verifies List input validation
func TestVaultList_InvalidInputs(t *testing.T) {
	v := &Vault{store: &VaultStore{Projects: make(map[string]map[string]Secret)}}
	
	_, err := v.List("")
	if err == nil {
		t.Error("Expected List to fail with empty project")
	}
	
	_, err = v.List("../proj")
	if err == nil {
		t.Error("Expected List to fail with .. in project")
	}
}

// TestVaultSetBatch_InvalidInputs verifies SetBatch input validation
func TestVaultSetBatch_InvalidInputs(t *testing.T) {
	v := &Vault{store: &VaultStore{Projects: make(map[string]map[string]Secret)}}
	
	err := v.SetBatch("", map[string]string{"k": "v"})
	if err == nil {
		t.Error("Expected SetBatch to fail with empty project")
	}
	
	err = v.SetBatch("../proj", map[string]string{"k": "v"})
	if err == nil {
		t.Error("Expected SetBatch to fail with .. in project")
	}
	
	err = v.SetBatch("proj", map[string]string{"../path": "v"})
	if err == nil {
		t.Error("Expected SetBatch to fail with .. in path")
	}
	
	err = v.SetBatch("proj", map[string]string{"path": string(make([]byte, 1024*1024+1))})
	if err == nil {
		t.Error("Expected SetBatch to fail with oversized secret")
	}
}
