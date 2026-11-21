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
	"unicode/utf8"
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

// ErrSecretNotFound is returned (wrapped) by Get when the secret does not exist.
var ErrSecretNotFound = errors.New("secret not found")

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
		if strings.TrimSpace(key) == "" {
			return "", errors.New("DSO_MASTER_KEY is set but blank; set it to a real key or unset it")
		}
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
	key := strings.TrimSpace(string(data))
	if key == "" {
		// An empty key would still "work" (Argon2id accepts it) and silently
		// encrypt the vault under a publicly known key. Never allow that. This
		// is what an interrupted `docker dso init` of an older version left
		// behind.
		return "", fmt.Errorf("master key file %s is empty; refusing to use an empty key. "+
			"If no vault exists yet, delete the file and re-run 'docker dso init'. "+
			"If %s already exists it may have been created under an empty key by an older version: "+
			"back up both files before changing anything", keyPath, filepath.Join(filepath.Dir(keyPath), "vault.enc"))
	}
	return key, nil
}

func generateMasterKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// lockTimeout bounds how long a writer waits for another dso process.
var lockTimeout = 30 * time.Second

// Hooks so tests can inject failures at the exact points where a real crash or
// I/O error could occur. Production code never reassigns them.
var (
	renameFile = os.Rename
	linkFile   = os.Link
	syncFile   = func(f *os.File) error { return f.Sync() }
	syncDir    = syncDirectory
)

// DurabilityError reports that a vault update WAS committed -- the new file is
// renamed into place and visible to every reader -- but a later step needed to
// make it durable across power loss (fsync of the directory) failed. It is
// distinct from every pre-commit failure, after which the previous vault is
// untouched. Callers can use IsCommitted to tell the two apart.
type DurabilityError struct{ Err error }

func (e *DurabilityError) Error() string {
	return "vault update committed but durability not confirmed: " + e.Err.Error()
}
func (e *DurabilityError) Unwrap() error { return e.Err }

// IsCommitted reports whether err means the update is in place on disk even
// though the operation returned an error (see DurabilityError).
func IsCommitted(err error) bool {
	var de *DurabilityError
	return errors.As(err, &de)
}

// InitDefault creates the .dso directory, a master key if one doesn't exist, and an empty vault.
// The whole check-then-create sequence runs under the vault lock: without it
// two concurrent inits could each generate a master key, and the loser's key
// would overwrite the winner's, leaving a vault encrypted under a lost key.
func InitDefault() error {
	dir, err := getVaultDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create vault dir: %w", err)
	}

	vaultPath := filepath.Join(dir, "vault.enc")
	release, err := acquireFileLock(vaultPath+".lock", lockTimeout)
	if err != nil {
		return fmt.Errorf("failed to lock vault: %w", err)
	}
	defer func() { _ = release() }()

	keyPath := filepath.Join(dir, "master.key")
	if _, err := os.Stat(keyPath); errors.Is(err, fs.ErrNotExist) {
		key, err := generateMasterKey()
		if err != nil {
			return err
		}
		if err := installMasterKey(dir, keyPath, key); err != nil {
			return err
		}
	}

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
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.saveLocked() // lock already held above
	}

	return nil
}

// installMasterKey publishes key at keyPath all-or-nothing: the key is written
// to a unique 0600 temp file in the same directory, fsynced, and only then made
// visible under its final name with link(2), which fails if the name already
// exists. A crash at any point therefore leaves either no master.key or a
// complete one -- never a partial or empty file that a later run could mistake
// for a valid key -- and an existing key is never replaced.
//
// On filesystems without hard links it falls back to rename(2); that is safe
// here because the caller holds the vault lock and has just seen keyPath absent.
func installMasterKey(dir, keyPath, key string) error {
	tmp, err := os.CreateTemp(dir, ".master.key.tmp-*")
	if err != nil {
		return fmt.Errorf("failed to write master key: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to secure master key file: %w", err)
	}
	if _, err := tmp.WriteString(key + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write master key: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to sync master key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to write master key: %w", err)
	}
	if err := linkFile(tmpName, keyPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil // another initializer won; keep its key
		}
		if err := renameFile(tmpName, keyPath); err != nil {
			return fmt.Errorf("failed to install master key: %w", err)
		}
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("master key installed but directory sync failed: %w", err)
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

	store, err := readStore(vaultPath, masterKey)
	if err != nil {
		return nil, err
	}

	return &Vault{
		store:     store,
		vaultPath: vaultPath,
		masterKey: masterKey,
	}, nil
}

// readStore reads, decrypts and integrity-checks the vault file at path.
func readStore(vaultPath, masterKey string) (*VaultStore, error) {
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
	return &store, nil
}

// Save securely encrypts and atomically writes the vault to disk.
//
// Save persists the in-memory state wholesale (it does NOT merge changes other
// processes made since this Vault was loaded); use Set/SetBatch for
// read-modify-write updates that are safe against concurrent writers.
func (v *Vault) Save() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.vaultPath == "" {
		return errors.New("vault has no backing path")
	}
	release, err := acquireFileLock(v.vaultPath+".lock", lockTimeout)
	if err != nil {
		return fmt.Errorf("failed to lock vault: %w", err)
	}
	defer func() { _ = release() }()
	return v.saveLocked()
}

// mutate runs fn as one atomic read-modify-write transaction across
// processes: take the exclusive file lock, RELOAD the vault from disk (so
// writes committed by other processes since this Vault was loaded are kept),
// apply fn, and save -- all before releasing the lock. If fn or the save fails
// before the atomic rename, the previous vault is untouched and the in-memory
// state is left as it was, so a later Save cannot persist a half-applied
// change. A failure AFTER the rename (directory fsync) is different: the new
// vault is already committed, memory keeps matching it, and the returned error
// satisfies IsCommitted.
func (v *Vault) mutate(fn func(*VaultStore) error) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.vaultPath == "" {
		return errors.New("vault has no backing path")
	}

	release, err := acquireFileLock(v.vaultPath+".lock", lockTimeout)
	if err != nil {
		return fmt.Errorf("failed to lock vault: %w", err)
	}
	defer func() { _ = release() }()

	if _, statErr := os.Stat(v.vaultPath); statErr == nil {
		fresh, err := readStore(v.vaultPath, v.masterKey)
		if err != nil {
			return err // never overwrite a vault we cannot read
		}
		v.store = fresh
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("failed to stat vault: %w", statErr)
	}
	working := cloneStore(v.store)
	if err := fn(working); err != nil {
		return err // v.store still mirrors disk
	}
	applied := v.store
	v.store = working
	if err := v.saveLocked(); err != nil {
		if IsCommitted(err) {
			// The new vault is already in place on disk, so memory must keep
			// matching it. The caller gets the error (durability unconfirmed).
			return err
		}
		v.store = applied // not committed: the previous vault is untouched
		return err
	}
	return nil
}

func cloneStore(in *VaultStore) *VaultStore {
	out := &VaultStore{Version: in.Version, Metadata: map[string]string{}, Projects: map[string]map[string]Secret{}}
	for k, val := range in.Metadata {
		out.Metadata[k] = val
	}
	for name, proj := range in.Projects {
		cp := make(map[string]Secret, len(proj))
		for path, sec := range proj {
			meta := make(map[string]string, len(sec.Meta))
			for mk, mv := range sec.Meta {
				meta[mk] = mv
			}
			if sec.Meta == nil {
				meta = nil
			}
			cp[path] = Secret{Value: sec.Value, Meta: meta}
		}
		out.Projects[name] = cp
	}
	return out
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

	return writeFileAtomic(v.vaultPath, ciphertext, 0600)
}

// writeFileAtomic replaces path with data so that a reader (or a crash) sees
// either the old complete file or the new complete file, never a partial one:
// write to a unique temp file in the same directory, flush it to stable
// storage, rename over the target, then fsync the directory so the rename
// itself is durable. Every step's error is returned. The temp file is removed
// on any failure before the rename.
//
// Durability caveat: fsync is the strongest guarantee POSIX offers, but
// whether data survives power loss still depends on the filesystem and
// storage honoring it (write caches, some network filesystems). This function
// does not and cannot promise more than that.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp vault file: %w", err)
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("failed to set temp vault file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temp vault file: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		return fmt.Errorf("failed to sync temp vault file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp vault file: %w", err)
	}
	if err := renameFile(tmpName, path); err != nil {
		return fmt.Errorf("failed to commit vault file: %w", err)
	}
	renamed = true
	if err := syncDir(dir); err != nil {
		// The new file is in place but its directory entry may not be durable.
		// Report it: success here would overstate the guarantee.
		return &DurabilityError{Err: fmt.Errorf("directory sync failed: %w", err)}
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
	return Secret{}, fmt.Errorf("%w: %s/%s", ErrSecretNotFound, project, path)
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
	if !utf8.ValidString(value) {
		return errors.New("secret value is not valid UTF-8; the vault stores text only")
	}

	return v.mutate(func(st *VaultStore) error {
		if _, ok := st.Projects[project]; !ok {
			st.Projects[project] = make(map[string]Secret)
		}
		st.Projects[project][path] = Secret{
			Value: value,
			Meta: map[string]string{
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			},
		}
		return nil
	})
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

	// Validate everything before touching state so a bad entry cannot leave
	// a partially applied batch.
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
	}

	return v.mutate(func(st *VaultStore) error {
		if _, ok := st.Projects[project]; !ok {
			st.Projects[project] = make(map[string]Secret)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for path, value := range secrets {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			st.Projects[project][path] = Secret{
				Value: value,
				Meta:  map[string]string{"updated_at": now},
			}
		}
		return nil
	})
}
