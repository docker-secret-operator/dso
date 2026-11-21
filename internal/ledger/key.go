package ledger

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// This file is the portable part of S3 host-local HMAC key management (design
// rev 6.3 §4; docs/design/m1-ledger-reconciliation.md §6 and §12.4): the file
// format, key id derivation, typed errors and the redacting key type. The
// filesystem part is in key_linux.go. Nothing here is wired into the agent.

const (
	// DefaultKeyDir is the persistent ledger directory (design O-8). It is never
	// /run/dso, which is tmpfs and would lose the key on reboot.
	DefaultKeyDir = "/var/lib/dso/ledger"

	keyFileName    = "ledger.key"
	keyMarkerName  = "ledger.key.id"
	keyTmpPrefix   = "ledger.key.tmp-"
	keyLossPrefix  = "ledger.key.keyloss-"
	ledgerFileName = "ledger.json"
	keyFileHeader  = "dso-ledger-key-v1"
	keyIDDomain    = "dso-ledger-key-id-v1"
	keyFileMode    = 0o400
	keyDirMode     = 0o700
	keyFileSize    = len(keyFileHeader) + 1 + 18 + 1 + 44 + 1 // header\nk-<16hex>\n<base64(32)>\n
	keyMarkerSize  = 18 + 1
)

// Typed key errors. They are matched with errors.Is; the concrete error is a
// *KeyError that may name the key id and a static reason, never key bytes.
var (
	// ErrKeyAbsent means this is a verified first run: no key file, no ledger
	// file and no evidence of a prior key. Only then may a key be generated.
	ErrKeyAbsent = errors.New("ledger key: absent (first run)")
	// ErrKeyLost means a key must have existed (a ledger, a key-id marker or a
	// preserved key is present) but the key file is missing. Never regenerated.
	ErrKeyLost = errors.New("ledger key: lost")
	// ErrKeyUnsafe means a key object exists but fails an ownership, permission,
	// type, link-count or directory check. Never regenerated, renamed or removed.
	ErrKeyUnsafe = errors.New("ledger key: unsafe")
	// ErrKeyCorrupt means a safe key file is not a valid key (wrong size,
	// truncated, malformed, id mismatch). Never regenerated, renamed or removed.
	ErrKeyCorrupt = errors.New("ledger key: corrupt")
	// ErrKeyExists means Generate found a usable key; it never replaces one.
	ErrKeyExists = errors.New("ledger key: a key already exists")
	// ErrKeyDirectory means the key directory configuration is refused.
	ErrKeyDirectory = errors.New("ledger key: invalid key directory")
	// ErrKeyUnsupported means key management is not available on this platform.
	ErrKeyUnsupported = errors.New("ledger key: unsupported platform")
	// ErrKeyCreate means a new key could not be created durably.
	ErrKeyCreate = errors.New("ledger key: creation failed")
)

// KeyError is the concrete key error. It never carries key material.
type KeyError struct {
	Kind   error  // one of the Err* sentinels
	KeyID  string // the key id, when known (derived, non-reversible)
	Reason string // static description of the failed check
	Err    error  // underlying system error, never derived from key bytes
}

func (e *KeyError) Error() string {
	var b strings.Builder
	b.WriteString(e.Kind.Error())
	if e.Reason != "" {
		b.WriteString(": ")
		b.WriteString(e.Reason)
	}
	if e.KeyID != "" {
		b.WriteString(" (key_id=")
		b.WriteString(e.KeyID)
		b.WriteString(")")
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap exposes both the sentinel and the system error to errors.Is/As.
func (e *KeyError) Unwrap() []error { return []error{e.Kind, e.Err} }

func keyErr(kind error, reason string, err error) *KeyError {
	return &KeyError{Kind: kind, Reason: reason, Err: err}
}

// DeriveKeyID returns "k-" + hex(first 8 bytes of SHA-256("dso-ledger-key-id-v1"
// || key)) (design §4 item 2). It is non-reversible and domain separated from
// the HMAC use of the key.
func DeriveKeyID(key []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(keyIDDomain))
	_, _ = h.Write(key)
	return "k-" + hex.EncodeToString(h.Sum(nil)[:8])
}

// HMACKey is a loaded host-local key. It redacts itself under every fmt verb,
// in JSON/text marshalling and in slog, so it cannot reach a log, an error or
// an audit record by accident. Bytes returns a copy.
type HMACKey struct {
	id  string
	key []byte
}

// ID returns the key id.
func (k HMACKey) ID() string { return k.id }

// Bytes returns a copy of the key material. Callers must not log it.
func (k HMACKey) Bytes() []byte { return append([]byte(nil), k.key...) }

// Zero overwrites the held key bytes.
func (k HMACKey) Zero() {
	for i := range k.key {
		k.key[i] = 0
	}
}

func (k HMACKey) redacted() string { return "ledger.HMACKey{id:" + k.id + ", key:[REDACTED]}" }

// String implements fmt.Stringer.
func (k HMACKey) String() string { return k.redacted() }

// GoString implements fmt.GoStringer.
func (k HMACKey) GoString() string { return k.redacted() }

// Format implements fmt.Formatter for every verb (%v %+v %#v %s %x %q %d ...).
func (k HMACKey) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, k.redacted()) }

// MarshalText implements encoding.TextMarshaler (and so JSON) without material.
func (k HMACKey) MarshalText() ([]byte, error) { return []byte(k.redacted()), nil }

// MarshalJSON emits a redacted JSON string.
func (k HMACKey) MarshalJSON() ([]byte, error) { return []byte(`"` + k.redacted() + `"`), nil }

// LogValue implements slog.LogValuer.
func (k HMACKey) LogValue() slog.Value { return slog.StringValue(k.redacted()) }

// encodeKeyFile returns the exact file content: header, key id, base64 key.
func encodeKeyFile(id string, key []byte) []byte {
	return []byte(keyFileHeader + "\n" + id + "\n" + base64.StdEncoding.EncodeToString(key) + "\n")
}

// decodeKeyFile parses and strictly validates file content. The error never
// contains content.
func decodeKeyFile(b []byte) (string, []byte, *KeyError) {
	if len(b) != keyFileSize {
		return "", nil, keyErr(ErrKeyCorrupt, fmt.Sprintf("key file has %d bytes, want %d", len(b), keyFileSize), nil)
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) != 4 || lines[3] != "" || lines[0] != keyFileHeader {
		return "", nil, keyErr(ErrKeyCorrupt, "key file header or layout is invalid", nil)
	}
	id := lines[1]
	if !ValidKeyID(id) {
		return "", nil, keyErr(ErrKeyCorrupt, "key file key id has an invalid format", nil)
	}
	key, err := base64.StdEncoding.Strict().DecodeString(lines[2])
	if err != nil || len(key) != HMACKeySize || base64.StdEncoding.EncodeToString(key) != lines[2] {
		return "", nil, keyErr(ErrKeyCorrupt, "key file key encoding is invalid", nil)
	}
	if want := DeriveKeyID(key); subtle.ConstantTimeCompare([]byte(want), []byte(id)) != 1 {
		for i := range key {
			key[i] = 0
		}
		return "", nil, keyErr(ErrKeyCorrupt, "key file key id does not match the key", nil)
	}
	return id, key, nil
}

// resolveLoose resolves symbolic links component by component, including
// dangling ones (filepath.EvalSymlinks fails on those), so a link that points
// into a not-yet-existing /run/dso is still seen.
func resolveLoose(p string) string {
	cur := string(filepath.Separator)
	rest := strings.Split(strings.TrimPrefix(filepath.Clean(p), "/"), "/")
	for links := 0; len(rest) > 0 && links < 40; {
		part := rest[0]
		rest = rest[1:]
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		if fi, err := os.Lstat(next); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			links++
			target, err := os.Readlink(next)
			if err != nil {
				return next
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(cur, target)
			}
			rest = append(strings.Split(strings.TrimPrefix(filepath.Clean(target), "/"), "/"), rest...)
			cur = string(filepath.Separator)
			continue
		}
		cur = next
	}
	return filepath.Join(append([]string{cur}, rest...)...)
}

// refuseVolatileDir rejects a key directory under /run/dso (tmpfs: the key would
// not survive a reboot while the ledger does), also through symbolic links.
func refuseVolatileDir(dir string) error {
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return keyErr(ErrKeyDirectory, "cannot resolve the directory path", err)
	}
	for _, c := range []string{abs, resolveLoose(abs)} {
		for _, v := range []string{"/run/dso", "/var/run/dso"} {
			if c == v || strings.HasPrefix(c, v+"/") {
				return keyErr(ErrKeyDirectory, "the key directory must be persistent, not under /run/dso", nil)
			}
		}
	}
	return nil
}
