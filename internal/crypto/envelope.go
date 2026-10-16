// Package crypto implements the envelope-encryption primitives used by the
// central secrets store: per-project data-encryption keys (DEKs) that encrypt
// secret versions, and a KeyProvider that protects the DEKs at rest.
//
// Nothing in this package logs, and no error returned from it contains key
// material, plaintext or ciphertext.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// KeySize is the size of every key in the hierarchy (AES-256).
	KeySize = 32
	// NonceSize is the GCM nonce size; every Seal uses a fresh random nonce.
	NonceSize = 12
	// MaxPlaintextSize bounds a single secret value.
	MaxPlaintextSize = 1 << 20
)

var (
	// ErrDecrypt is returned for every authentication or format failure when
	// opening a ciphertext. It is deliberately uninformative: wrong key,
	// tampered ciphertext and mismatched AAD are indistinguishable.
	ErrDecrypt = errors.New("crypto: decryption failed (wrong key, corrupted data, or data bound to a different context)")
	// ErrInvalidKey is returned for a key of the wrong size.
	ErrInvalidKey = errors.New("crypto: invalid key")
	// ErrPlaintextTooLarge is returned by Seal for oversized values.
	ErrPlaintextTooLarge = errors.New("crypto: value exceeds maximum size")
)

// randReader is replaced in tests to simulate entropy failure.
var randReader io.Reader = rand.Reader

// GenerateKey returns a new random 256-bit key.
func GenerateKey() ([]byte, error) {
	k := make([]byte, KeySize)
	if _, err := io.ReadFull(randReader, k); err != nil {
		return nil, fmt.Errorf("crypto: reading random bytes: %w", err)
	}
	return k, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidKey
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext under key with AES-256-GCM, authenticating aad. A
// fresh random 96-bit nonce is generated per call and returned separately; the
// returned ciphertext includes the GCM tag.
func Seal(key, plaintext, aad []byte) (nonce, ciphertext []byte, err error) {
	if len(plaintext) > MaxPlaintextSize {
		return nil, nil, ErrPlaintextTooLarge
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, NonceSize)
	if _, err := io.ReadFull(randReader, nonce); err != nil {
		return nil, nil, fmt.Errorf("crypto: reading random nonce: %w", err)
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, aad), nil
}

// Open authenticates and decrypts. Any failure returns ErrDecrypt.
func Open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceSize || len(ciphertext) < gcm.Overhead() {
		return nil, ErrDecrypt
	}
	pt, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// Zero overwrites b. It is best-effort hygiene: Go may have copied the slice.
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// aad builds an unambiguous, length-prefixed associated-data encoding so that
// ("ab","c") and ("a","bc") can never collide.
func aad(domain string, fields ...string) []byte {
	out := make([]byte, 0, 64)
	out = appendField(out, domain)
	for _, f := range fields {
		out = appendField(out, f)
	}
	return out
}

func appendField(b []byte, s string) []byte {
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(s)))
	b = append(b, l[:]...)
	return append(b, s...)
}

// SecretAAD is the authenticated context of one secret version. Binding it to
// the ciphertext makes a ciphertext valid only for exactly this project,
// environment, secret and version, so database-level row swaps fail closed.
func SecretAAD(projectID, environmentID, secretID, name string, version int, dekVersion int) []byte {
	return aad("dso/secret-version/v1", projectID, environmentID, secretID, name,
		fmt.Sprintf("v%d", version), fmt.Sprintf("dek%d", dekVersion))
}

// DEKAAD is the authenticated context of a wrapped project DEK.
func DEKAAD(projectID, keyID string, dekVersion int) []byte {
	return aad("dso/project-dek/v1", projectID, keyID, fmt.Sprintf("dek%d", dekVersion))
}
