package vault

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Secret struct {
	Value string            `json:"value"`
	Meta  map[string]string `json:"_meta,omitempty"`
}

type VaultStore struct {
	Version  string                       `json:"version"`
	Metadata map[string]string            `json:"metadata"`
	Projects map[string]map[string]Secret `json:"projects"`
}

type Vault struct {
	mu        sync.RWMutex
	store     *VaultStore
	vaultPath string
	masterKey string
}

func getVaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dso"), nil
}

// getMasterKey resolves the master key from the environment variable or file.
func getMasterKey() (string, error) {
	if key := os.Getenv("DSO_MASTER_KEY"); key != "" {
		return key, nil
	}

	dir, err := getVaultDir()
	if err != nil {
		return "", err
	}
	keyPath := filepath.Join(dir, "master.key")

	data, err := os.ReadFile(keyPath) // #nosec G304 -- keyPath is always under the current user's DSO vault directory.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("master key not found at %s and DSO_MASTER_KEY not set", keyPath)
		}
		return "", fmt.Errorf("failed to read master key: %w", err)
	}
	// Enforce restrictive permissions — fix silently if a previous version wrote it wrong.
	if err := os.Chmod(keyPath, 0600); err != nil {
		return "", fmt.Errorf("failed to secure master key file permissions: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func generateMasterKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// InitDefault creates the .dso directory, a master key if one doesn't exist, and an empty vault.
func InitDefault() error {
	dir, err := getVaultDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create vault dir: %w", err)
	}

	keyPath := filepath.Join(dir, "master.key")
	if _, err := os.Stat(keyPath); errors.Is(err, fs.ErrNotExist) {
		key, err := generateMasterKey()
		if err != nil {
			return err
		}
		if err := os.WriteFile(keyPath, []byte(key+"\n"), 0600); err != nil {
			return fmt.Errorf("failed to write master key: %w", err)
		}
	}

	vaultPath := filepath.Join(dir, "vault.enc")
	if _, err := os.Stat(vaultPath); errors.Is(err, fs.ErrNotExist) {
		v := &Vault{
			store: &VaultStore{
				Version:  "1.0",
				Metadata: map[string]string{"created_at": time.Now().UTC().Format(time.RFC3339)},
				Projects: make(map[string]map[string]Secret),
			},
			vaultPath: vaultPath,
		}
		masterKey, err := getMasterKey()
		if err != nil {
			return err
		}
		v.masterKey = masterKey
		return v.Save() // Save will encrypt and create the file securely
	}

	return nil
}

// LoadDefault reads, decrypts, and validates the default vault.
func LoadDefault() (*Vault, error) {
	dir, err := getVaultDir()
	if err != nil {
		return nil, err
	}
	vaultPath := filepath.Join(dir, "vault.enc")

	masterKey, err := getMasterKey()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(vaultPath) // #nosec G304 -- vaultPath is always under the current user's DSO vault directory.
	if err != nil {
		return nil, fmt.Errorf("failed to read vault: %w", err)
	}

	plaintext, err := Decrypt(data, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt vault: %w", err)
	}

	var store VaultStore
	if err := json.Unmarshal(plaintext, &store); err != nil {
		return nil, fmt.Errorf("failed to parse vault data: %w", err)
	}

	// Validate checksum if it exists
	if expectedChecksum, ok := store.Metadata["checksum"]; ok {
		delete(store.Metadata, "checksum")
		bytesToHash, _ := json.Marshal(store)
		actualChecksum := fmt.Sprintf("sha256:%x", sha256.Sum256(bytesToHash))
		if expectedChecksum != actualChecksum {
			return nil, errors.New("vault integrity check failed: checksum mismatch")
		}
		store.Metadata["checksum"] = expectedChecksum
	}

	if store.Projects == nil {
		store.Projects = make(map[string]map[string]Secret)
	}
	if store.Metadata == nil {
		store.Metadata = make(map[string]string)
	}

	return &Vault{
		store:     &store,
		vaultPath: vaultPath,
		masterKey: masterKey,
	}, nil
}

// Save securely encrypts and atomically writes the vault to disk.
func (v *Vault) Save() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.saveLocked()
}

func (v *Vault) saveLocked() error {
	if v.store.Metadata == nil {
		v.store.Metadata = make(map[string]string)
	}
	v.store.Metadata["last_updated"] = time.Now().UTC().Format(time.RFC3339)
	delete(v.store.Metadata, "checksum") // Exclude from checksum generation

	bytesToHash, err := json.Marshal(v.store)
	if err != nil {
		return fmt.Errorf("failed to marshal vault for checksum: %w", err)
	}
	v.store.Metadata["checksum"] = fmt.Sprintf("sha256:%x", sha256.Sum256(bytesToHash))

	plaintext, err := json.Marshal(v.store)
	if err != nil {
		return fmt.Errorf("failed to marshal vault: %w", err)
	}

	ciphertext, err := Encrypt(plaintext, v.masterKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt vault: %w", err)
	}

	// Atomic write
	tmpFile := v.vaultPath + ".tmp"
	if err := os.WriteFile(tmpFile, ciphertext, 0600); err != nil {
		return fmt.Errorf("failed to write temp vault file: %w", err)
	}

	if err := os.Rename(tmpFile, v.vaultPath); err != nil {
		_ = os.Remove(tmpFile) // clean up temp file on failure
		return fmt.Errorf("failed to commit vault file: %w", err)
	}

	return nil
}

// Get retrieves a secret from the vault safely.
func (v *Vault) Get(project, path string) (Secret, error) {
	project = strings.TrimSpace(project)
	path = strings.TrimSpace(path)
	if project == "" || path == "" {
		return Secret{}, errors.New("project and path cannot be empty")
	}
	if strings.Contains(project, "..") || strings.Contains(path, "..") {
		return Secret{}, errors.New("invalid path: contains '..'")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if proj, ok := v.store.Projects[project]; ok {
		if sec, ok := proj[path]; ok {
			return sec, nil
		}
	}
	return Secret{}, fmt.Errorf("secret not found: %s/%s", project, path)
}

// Set writes a secret to the vault and persists it.
func (v *Vault) Set(project, path, value string) error {
	project = strings.TrimSpace(project)
	path = strings.TrimSpace(path)
	if project == "" || path == "" {
		return errors.New("project and path cannot be empty")
	}
	if strings.Contains(project, "..") || strings.Contains(path, "..") {
		return errors.New("invalid path: contains '..'")
	}
	if len(value) > 1<<20 {
		return errors.New("secret exceeds max size of 1MB")
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if _, ok := v.store.Projects[project]; !ok {
		v.store.Projects[project] = make(map[string]Secret)
	}

	v.store.Projects[project][path] = Secret{
		Value: value,
		Meta: map[string]string{
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		},
	}

	return v.saveLocked()
}

// List returns all secret paths for a given project.
func (v *Vault) List(project string) ([]string, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return nil, errors.New("project cannot be empty")
	}
	if strings.Contains(project, "..") {
		return nil, errors.New("invalid project: contains '..'")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	proj, ok := v.store.Projects[project]
	if !ok {
		return []string{}, nil
	}

	paths := make([]string, 0, len(proj))
	for path := range proj {
		paths = append(paths, path)
	}
	return paths, nil
}

// SetBatch writes multiple secrets to a project in the vault and persists once.
func (v *Vault) SetBatch(project string, secrets map[string]string) error {
	project = strings.TrimSpace(project)
	if project == "" {
		return errors.New("project cannot be empty")
	}
	if strings.Contains(project, "..") {
		return errors.New("invalid project: contains '..'")
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if _, ok := v.store.Projects[project]; !ok {
		v.store.Projects[project] = make(map[string]Secret)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for path, value := range secrets {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if strings.Contains(path, "..") {
			return fmt.Errorf("invalid path: contains '..': %s", path)
		}
		if len(value) > 1<<20 {
			return fmt.Errorf("secret exceeds max size of 1MB: %s", path)
		}
		v.store.Projects[project][path] = Secret{
			Value: value,
			Meta: map[string]string{
				"updated_at": now,
			},
		}
	}

	return v.saveLocked()
}
