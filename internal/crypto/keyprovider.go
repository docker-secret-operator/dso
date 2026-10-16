package crypto

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/argon2"
)

// KeyProvider protects project DEKs with a root key that is never stored in
// the database. Wrap/Unwrap (rather than "give me the key") is the contract so
// a KMS implementation, where the root key never leaves the service, can be
// added without changing any storage logic.
//
// Implementations must fail closed: any error means no key material is
// returned. They must not log or include key material in errors.
type KeyProvider interface {
	// ID is a stable, non-secret identifier of the root key. It is recorded
	// with every wrapped DEK so the wrong root key is detected up front.
	ID() string
	// Wrap encrypts a DEK, authenticating aad.
	Wrap(ctx context.Context, dek, aad []byte) ([]byte, error)
	// Unwrap reverses Wrap. Wrong key, tampering or aad mismatch → error.
	Unwrap(ctx context.Context, wrapped, aad []byte) ([]byte, error)
}

// ErrKeyMaterial is returned for missing, malformed or insecure key material.
var ErrKeyMaterial = errors.New("crypto: invalid root key material")

// ErrWrongPassphrase is returned when a passphrase does not match the key file.
var ErrWrongPassphrase = errors.New("crypto: wrong passphrase")

// localProvider wraps DEKs with an in-memory 256-bit KEK. Both the file and
// the passphrase providers are this type; they differ only in how the KEK is
// obtained.
type localProvider struct {
	kek []byte
	id  string
}

func keyID(kek []byte) string {
	h := sha256.Sum256(append([]byte("dso/root-key-id/v1"), kek...))
	return hex.EncodeToString(h[:8])
}

func (p *localProvider) ID() string { return p.id }

func (p *localProvider) Wrap(_ context.Context, dek, aad []byte) ([]byte, error) {
	if len(dek) != KeySize {
		return nil, ErrInvalidKey
	}
	nonce, ct, err := Seal(p.kek, dek, aad)
	if err != nil {
		return nil, err
	}
	return append(nonce, ct...), nil
}

func (p *localProvider) Unwrap(_ context.Context, wrapped, aad []byte) ([]byte, error) {
	if len(wrapped) < NonceSize {
		return nil, ErrDecrypt
	}
	dek, err := Open(p.kek, wrapped[:NonceSize], wrapped[NonceSize:], aad)
	if err != nil {
		return nil, err
	}
	if len(dek) != KeySize {
		Zero(dek)
		return nil, ErrDecrypt
	}
	return dek, nil
}

// ---- file provider -------------------------------------------------------

const fileKeyPrefix = "dso-root-key-v1:"

// InstallKeyFile generates a new random root key and installs it at path. It
// never overwrites: the key is written 0400 to a unique temporary file in the
// same directory, fsynced, and hard-linked into place (link(2) fails if path
// exists), so a crash or a concurrent init can never replace or truncate an
// existing key. The directory is created 0700 if needed.
func InstallKeyFile(path string) (keyIDOut string, err error) {
	kek, err := GenerateKey()
	if err != nil {
		return "", err
	}
	defer Zero(kek)
	content := []byte(fileKeyPrefix + base64.RawStdEncoding.EncodeToString(kek) + "\n")
	defer Zero(content)
	if err := installExclusive(path, content); err != nil {
		return "", err
	}
	return keyID(kek), nil
}

func installExclusive(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating key directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".dso-key-*")
	if err != nil {
		return fmt.Errorf("creating temporary key file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o400); err != nil {
		tmp.Close()
		return fmt.Errorf("restricting temporary key file: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing key file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing key file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("key file %s already exists; refusing to overwrite", path)
		}
		return fmt.Errorf("installing key file: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// readKeyFile opens path without following symlinks, requires a regular file
// owned by the current user and not accessible by group/other, and returns its
// contents.
func readKeyFile(path string) ([]byte, error) {
	f, err := openNoFollow(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyMaterial, sanitizeOpenErr(err))
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyMaterial, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrKeyMaterial, path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s has mode %04o; it must not be accessible by group or others (chmod 400 %s)",
			ErrKeyMaterial, path, fi.Mode().Perm(), path)
	}
	if !ownedByCurrentUser(fi) {
		return nil, fmt.Errorf("%w: %s is not owned by the current user", ErrKeyMaterial, path)
	}
	if fi.Size() > 1<<16 {
		return nil, fmt.Errorf("%w: %s is too large", ErrKeyMaterial, path)
	}
	b, err := io.ReadAll(io.LimitReader(f, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyMaterial, err)
	}
	return b, nil
}

func sanitizeOpenErr(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "key file does not exist"
	}
	return err.Error()
}

// NewFileKeyProvider loads a root key installed by InstallKeyFile.
func NewFileKeyProvider(path string) (KeyProvider, error) {
	raw, err := readKeyFile(path)
	if err != nil {
		return nil, err
	}
	defer Zero(raw)
	s := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(s, fileKeyPrefix) {
		return nil, fmt.Errorf("%w: unrecognised key file format", ErrKeyMaterial)
	}
	kek, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, fileKeyPrefix))
	if err != nil || len(kek) != KeySize {
		return nil, fmt.Errorf("%w: malformed key", ErrKeyMaterial)
	}
	return &localProvider{kek: kek, id: keyID(kek)}, nil
}

// ---- passphrase provider -------------------------------------------------

// MinPassphraseLength is enforced at init time only (an existing key file must
// remain usable).
const MinPassphraseLength = 12

// Argon2id parameter floors/ceilings. Floors stop a tampered parameter file
// from silently downgrading the KDF; ceilings stop it from exhausting memory.
const (
	argonTime       = 3
	argonMemoryKiB  = 64 * 1024
	argonThreads    = 4
	minArgonTime    = 2
	minArgonMemKiB  = 19 * 1024
	maxArgonTime    = 12
	maxArgonMemKiB  = 1024 * 1024
	passphraseFmtV1 = "dso-passphrase-key-v1"
)

type passphraseFile struct {
	Format  string `json:"format"`
	KDF     string `json:"kdf"`
	Salt    string `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory_kib"`
	Threads uint8  `json:"threads"`
	KeyID   string `json:"key_id"`
}

func deriveKEK(passphrase []byte, salt []byte, t, m uint32, p uint8) []byte {
	return argon2.IDKey(passphrase, salt, t, m, p, KeySize)
}

// InstallPassphraseKeyFile creates the KDF parameter file for a passphrase
// root key at path (random salt, Argon2id parameters, key-id check value). The
// passphrase itself is never stored. Installation is exclusive and atomic like
// InstallKeyFile.
func InstallPassphraseKeyFile(path string, passphrase []byte) (string, error) {
	if len(passphrase) < MinPassphraseLength {
		return "", fmt.Errorf("passphrase must be at least %d characters", MinPassphraseLength)
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(randReader, salt); err != nil {
		return "", fmt.Errorf("crypto: reading random salt: %w", err)
	}
	kek := deriveKEK(passphrase, salt, argonTime, argonMemoryKiB, argonThreads)
	defer Zero(kek)
	pf := passphraseFile{
		Format: passphraseFmtV1, KDF: "argon2id", Salt: base64.RawStdEncoding.EncodeToString(salt),
		Time: argonTime, Memory: argonMemoryKiB, Threads: argonThreads, KeyID: keyID(kek),
	}
	b, _ := json.MarshalIndent(pf, "", "  ")
	if err := installExclusive(path, append(b, '\n')); err != nil {
		return "", err
	}
	return pf.KeyID, nil
}

// NewPassphraseKeyProvider derives the root key from passphrase and the
// parameter file at path. A wrong passphrase yields ErrWrongPassphrase.
func NewPassphraseKeyProvider(path string, passphrase []byte) (KeyProvider, error) {
	raw, err := readKeyFile(path)
	if err != nil {
		return nil, err
	}
	var pf passphraseFile
	if err := json.Unmarshal(raw, &pf); err != nil || pf.Format != passphraseFmtV1 || pf.KDF != "argon2id" {
		return nil, fmt.Errorf("%w: unrecognised passphrase key file format", ErrKeyMaterial)
	}
	salt, err := base64.RawStdEncoding.DecodeString(pf.Salt)
	if err != nil || len(salt) < 16 {
		return nil, fmt.Errorf("%w: malformed salt", ErrKeyMaterial)
	}
	if pf.Time < minArgonTime || pf.Time > maxArgonTime ||
		pf.Memory < minArgonMemKiB || pf.Memory > maxArgonMemKiB || pf.Threads < 1 {
		return nil, fmt.Errorf("%w: KDF parameters outside the accepted range", ErrKeyMaterial)
	}
	if len(passphrase) == 0 {
		return nil, ErrWrongPassphrase
	}
	kek := deriveKEK(passphrase, salt, pf.Time, pf.Memory, pf.Threads)
	if keyID(kek) != pf.KeyID {
		Zero(kek)
		return nil, ErrWrongPassphrase
	}
	return &localProvider{kek: kek, id: pf.KeyID}, nil
}
