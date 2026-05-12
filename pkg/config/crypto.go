package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// CryptoManager handles encryption/decryption of sensitive credentials
type CryptoManager struct {
	masterKey []byte
}

// NewCryptoManager creates a crypto manager from a master key
// The master key should be 32 bytes for AES-256
func NewCryptoManager(masterKey []byte) (*CryptoManager, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}
	return &CryptoManager{masterKey: masterKey}, nil
}

// DeriveKeyFromPassword derives a 32-byte key from a password using Argon2id
func DeriveKeyFromPassword(password string, salt []byte) []byte {
	if len(salt) != 16 {
		h := sha256.Sum256([]byte("dso-default-salt"))
		salt = h[:16]
	}
	key := argon2.IDKey([]byte(password), salt, 2, 65536, 8, 32)
	return key
}

// Encrypt encrypts plaintext and returns base64-encoded ciphertext with IV prepended
func (cm *CryptoManager) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(cm.masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Base64 encode for storage in YAML
	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	return encoded, nil
}

// Decrypt decrypts base64-encoded ciphertext with IV and returns plaintext
func (cm *CryptoManager) Decrypt(ciphertext string) (string, error) {
	block, err := aes.NewCipher(cm.masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decode from base64
	ciphertextBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertextBytes) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := ciphertextBytes[:nonceSize], ciphertextBytes[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// EncryptSensitiveFields encrypts provider credentials in config
func (cm *CryptoManager) EncryptProviderConfig(config *Config) error {
	for name, provider := range config.Providers {
		if provider.Auth.Params == nil {
			provider.Auth.Params = make(map[string]string)
		}

		sensitiveFields := []string{"password", "api_key", "secret_key", "token", "access_key", "secret_access_key"}
		for _, field := range sensitiveFields {
			if val, ok := provider.Config[field]; ok && val != "" {
				encrypted, err := cm.Encrypt(val)
				if err != nil {
					return fmt.Errorf("failed to encrypt field %s in provider %s: %w", field, name, err)
				}
				// Mark as encrypted with prefix
				provider.Config[field] = "enc:" + encrypted
			}
		}

		// Also encrypt auth params
		for key, val := range provider.Auth.Params {
			if val != "" {
				encrypted, err := cm.Encrypt(val)
				if err != nil {
					return fmt.Errorf("failed to encrypt auth param %s in provider %s: %w", key, name, err)
				}
				provider.Auth.Params[key] = "enc:" + encrypted
			}
		}

		config.Providers[name] = provider
	}
	return nil
}

// DecryptSensitiveFields decrypts provider credentials in config
func (cm *CryptoManager) DecryptProviderConfig(config *Config) error {
	for _, provider := range config.Providers {
		if provider.Config == nil {
			provider.Config = make(map[string]string)
		}

		for key, val := range provider.Config {
			if len(val) > 4 && val[:4] == "enc:" {
				decrypted, err := cm.Decrypt(val[4:])
				if err != nil {
					return fmt.Errorf("failed to decrypt config field %s: %w", key, err)
				}
				provider.Config[key] = decrypted
			}
		}

		for key, val := range provider.Auth.Params {
			if len(val) > 4 && val[:4] == "enc:" {
				decrypted, err := cm.Decrypt(val[4:])
				if err != nil {
					return fmt.Errorf("failed to decrypt auth param %s: %w", key, err)
				}
				provider.Auth.Params[key] = decrypted
			}
		}
	}
	return nil
}
