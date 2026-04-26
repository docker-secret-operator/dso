package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	saltSize     = 16
	nonceSize    = 12 // Standard for GCM
	keySize      = 32 // 256 bits for AES-256
	argonTime    = 3
	argonMem     = 128 * 1024
	argonThreads = 4
)

// deriveKey uses Argon2id to derive a 256-bit key from the master key and salt.
func deriveKey(masterKey string, salt []byte) []byte {
	return argon2.IDKey([]byte(masterKey), salt, argonTime, argonMem, argonThreads, keySize)
}

// Encrypt encrypts the plaintext using AES-256-GCM and the derived key.
// The resulting ciphertext has the format: salt (16 bytes) + nonce (12 bytes) + encrypted_data
func Encrypt(plaintext []byte, masterKey string) ([]byte, error) {
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	key := deriveKey(masterKey, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nil, nonce, plaintext, nil)

	// Combine salt + nonce + ciphertext
	out := make([]byte, 0, len(salt)+len(nonce)+len(ciphertext))
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)

	return out, nil
}

// Decrypt decrypts data encrypted by Encrypt.
func Decrypt(data []byte, masterKey string) ([]byte, error) {
	if len(data) < saltSize+nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	salt := data[:saltSize]
	nonce := data[saltSize : saltSize+nonceSize]
	ciphertext := data[saltSize+nonceSize:]

	key := deriveKey(masterKey, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (invalid key or corrupted data): %w", err)
	}

	return plaintext, nil
}
