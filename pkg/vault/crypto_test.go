package vault

import (
	"bytes"
	"crypto/rand"
	"testing"
)

// TestDeriveKeyDeterminism produces same key from same inputs
func TestDeriveKeyDeterminism(t *testing.T) {
	masterKey := "test-master-key-0123456789abcdef"
	salt := []byte("salt-16-bytes!!!")

	key1 := deriveKey(masterKey, salt)
	key2 := deriveKey(masterKey, salt)

	if !bytes.Equal(key1, key2) {
		t.Error("Key derivation is not deterministic")
	}

	if len(key1) != keySize {
		t.Errorf("Derived key size wrong. Expected %d, got %d", keySize, len(key1))
	}
}

// TestDeriveKeyDifferentInputs produces different keys
func TestDeriveKeyDifferentInputs(t *testing.T) {
	masterKey := "test-master-key-0123456789abcdef"
	salt1 := []byte("salt1-16-bytes!")
	salt2 := []byte("salt2-16-bytes!")

	key1 := deriveKey(masterKey, salt1)
	key2 := deriveKey(masterKey, salt2)

	if bytes.Equal(key1, key2) {
		t.Error("Different salts should produce different keys")
	}
}

// TestDeriveKeyWithDifferentMasterKeys produces different keys
func TestDeriveKeyWithDifferentMasterKeys(t *testing.T) {
	masterKey1 := "master-key-1234567890abcdef"
	masterKey2 := "master-key-fedcba0987654321"
	salt := []byte("same-salt-16!!!!")

	key1 := deriveKey(masterKey1, salt)
	key2 := deriveKey(masterKey2, salt)

	if bytes.Equal(key1, key2) {
		t.Error("Different master keys should produce different keys")
	}
}

// TestEncryptGeneratesRandomSalt creates different ciphertexts
func TestEncryptGeneratesRandomSalt(t *testing.T) {
	plaintext := []byte("test-plaintext")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext1, err1 := Encrypt(plaintext, masterKey)
	ciphertext2, err2 := Encrypt(plaintext, masterKey)

	if err1 != nil || err2 != nil {
		t.Fatalf("Encrypt failed: %v, %v", err1, err2)
	}

	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Error("Same plaintext should produce different ciphertexts (random salt/nonce)")
	}

	// But decryption should produce same plaintext
	plain1, _ := Decrypt(ciphertext1, masterKey)
	plain2, _ := Decrypt(ciphertext2, masterKey)

	if string(plain1) != string(plain2) || string(plain1) != string(plaintext) {
		t.Error("Decrypted plaintext mismatch")
	}
}

// TestEncryptCiphertextFormat validates structure
func TestEncryptCiphertextFormat(t *testing.T) {
	plaintext := []byte("test")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Format: salt (16) + nonce (12) + ciphertext
	expectedMinLen := saltSize + nonceSize + 1 // at least 1 byte of actual ciphertext
	if len(ciphertext) < expectedMinLen {
		t.Errorf("Ciphertext too short. Expected at least %d bytes, got %d", expectedMinLen, len(ciphertext))
	}

	// Extract and verify salt
	salt := ciphertext[:saltSize]
	if len(salt) != saltSize {
		t.Errorf("Salt extraction failed")
	}

	// Salt should not be all zeros (would be suspicious)
	allZeros := true
	for _, b := range salt {
		if b != 0 {
			allZeros = false
			break
		}
	}
	if allZeros {
		t.Error("Salt is all zeros (should be random)")
	}

	// Extract and verify nonce
	nonce := ciphertext[saltSize : saltSize+nonceSize]
	if len(nonce) != nonceSize {
		t.Errorf("Nonce extraction failed")
	}

	// Nonce should not be all zeros
	allZeros = true
	for _, b := range nonce {
		if b != 0 {
			allZeros = false
			break
		}
	}
	if allZeros {
		t.Error("Nonce is all zeros (should be random)")
	}
}

// TestEncryptEmpty handles zero-length plaintext
func TestEncryptEmpty(t *testing.T) {
	plaintext := []byte("")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Should still have salt + nonce even for empty plaintext
	if len(ciphertext) < saltSize+nonceSize {
		t.Error("Ciphertext too short for empty plaintext")
	}

	decrypted, err := Decrypt(ciphertext, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("Decrypted empty plaintext is not empty: %v", decrypted)
	}
}

// TestEncryptLarge handles large payloads
func TestEncryptLarge(t *testing.T) {
	plaintext := make([]byte, 1024*1024) // 1MB
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatalf("Failed to generate random plaintext: %v", err)
	}

	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("Large plaintext mismatch after roundtrip")
	}
}

// TestDecryptCorruptedSalt fails gracefully
func TestDecryptCorruptedSalt(t *testing.T) {
	plaintext := []byte("test")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, _ := Encrypt(plaintext, masterKey)

	// Corrupt salt (first 16 bytes)
	if len(ciphertext) > saltSize {
		corruptedCiphertext := make([]byte, len(ciphertext))
		copy(corruptedCiphertext, ciphertext)
		corruptedCiphertext[0] ^= 0xFF // Flip bits in salt

		decrypted, err := Decrypt(corruptedCiphertext, masterKey)
		if err != nil {
			// Expected to fail - wrong key derivation
			return
		}

		// Even if it doesn't fail, it should not produce original plaintext
		if bytes.Equal(decrypted, plaintext) {
			t.Error("Corrupted salt produced valid decryption (should have failed)")
		}
	}
}

// TestDecryptCorruptedNonce fails gracefully
func TestDecryptCorruptedNonce(t *testing.T) {
	plaintext := []byte("test")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, _ := Encrypt(plaintext, masterKey)

	// Corrupt nonce (bytes 16-28)
	if len(ciphertext) > saltSize+nonceSize {
		corruptedCiphertext := make([]byte, len(ciphertext))
		copy(corruptedCiphertext, ciphertext)
		corruptedCiphertext[saltSize] ^= 0xFF // Flip bits in nonce

		_, err := Decrypt(corruptedCiphertext, masterKey)
		if err == nil {
			t.Error("Corrupted nonce should fail decryption")
		}
	}
}

// TestDecryptCorruptedCiphertext fails gracefully
func TestDecryptCorruptedCiphertext(t *testing.T) {
	plaintext := []byte("test data for encryption")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, _ := Encrypt(plaintext, masterKey)

	// Corrupt actual ciphertext (after salt+nonce)
	if len(ciphertext) > saltSize+nonceSize+5 {
		corruptedCiphertext := make([]byte, len(ciphertext))
		copy(corruptedCiphertext, ciphertext)
		corruptedCiphertext[saltSize+nonceSize+5] ^= 0xFF // Flip bits in ciphertext

		_, err := Decrypt(corruptedCiphertext, masterKey)
		if err == nil {
			t.Error("Corrupted ciphertext should fail GCM authentication")
		}
	}
}

// TestEncryptBytesStability ensures encryption is stable
func TestEncryptBytesStability(t *testing.T) {
	plaintext := []byte("consistent plaintext")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// Encrypt multiple times - should produce different ciphertexts (due to random salt/nonce)
	// but all should decrypt to the same plaintext
	results := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		ct, err := Encrypt(plaintext, masterKey)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}
		results[i] = ct
	}

	// All should decrypt to original plaintext
	for i, ct := range results {
		decrypted, err := Decrypt(ct, masterKey)
		if err != nil {
			t.Fatalf("Decrypt %d failed: %v", i, err)
		}
		if !bytes.Equal(decrypted, plaintext) {
			t.Errorf("Decrypt %d produced wrong plaintext", i)
		}
	}

	// Ciphertexts should be different (due to random components)
	for i := 0; i < len(results)-1; i++ {
		if bytes.Equal(results[i], results[i+1]) {
			t.Error("Ciphertexts should be different (random salt/nonce)")
		}
	}
}

// TestKeyDerivationWithEmptySalt handles edge case
func TestKeyDerivationWithEmptySalt(t *testing.T) {
	masterKey := "test-key"
	salt := []byte("")

	key := deriveKey(masterKey, salt)

	if len(key) != keySize {
		t.Errorf("Key size wrong with empty salt. Expected %d, got %d", keySize, len(key))
	}

	// Empty salt should still produce deterministic output
	key2 := deriveKey(masterKey, salt)
	if !bytes.Equal(key, key2) {
		t.Error("Key derivation with empty salt should be deterministic")
	}
}

// TestKeyDerivationWithLongMasterKey handles long keys
func TestKeyDerivationWithLongMasterKey(t *testing.T) {
	longKey := string(make([]byte, 1000))
	salt := make([]byte, saltSize)
	rand.Read(salt)

	key := deriveKey(longKey, salt)

	if len(key) != keySize {
		t.Errorf("Key size wrong with long master key. Expected %d, got %d", keySize, len(key))
	}
}

// TestEncryptDecryptUnicodeContent handles unicode properly
func TestEncryptDecryptUnicodeContent(t *testing.T) {
	tests := []string{
		"Hello, World!",
		"مرحبا، العالم",     // Arabic
		"你好世界",              // Simplified Chinese
		"こんにちは世界",           // Japanese
		"🔐🔑🛡️",              // Emoji
		"Mixed: مرحبا 世界 🔐", // Mixed
	}

	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	for _, plaintext := range tests {
		plaintextBytes := []byte(plaintext)
		ciphertext, err := Encrypt(plaintextBytes, masterKey)
		if err != nil {
			t.Fatalf("Encrypt failed for %q: %v", plaintext, err)
		}

		decrypted, err := Decrypt(ciphertext, masterKey)
		if err != nil {
			t.Fatalf("Decrypt failed for %q: %v", plaintext, err)
		}

		if string(decrypted) != plaintext {
			t.Errorf("Unicode mismatch. Expected %q, got %q", plaintext, string(decrypted))
		}
	}
}

// TestEncryptWithHexEncodedKey handles hex-encoded keys
func TestEncryptWithHexEncodedKey(t *testing.T) {
	plaintext := []byte("test")

	// Valid hex key
	validHexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	ciphertext, err := Encrypt(plaintext, validHexKey)
	if err != nil {
		t.Fatalf("Encrypt with hex key failed: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, validHexKey)
	if err != nil {
		t.Fatalf("Decrypt with hex key failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("Plaintext mismatch with hex key")
	}
}

// TestArgonParameters validates cryptographic parameters
func TestArgonParameters(t *testing.T) {
	// These should be constant and appropriate for security
	if argonTime < 2 {
		t.Error("argonTime too low (minimum recommended: 2)")
	}

	if argonMem < 64*1024 {
		t.Error("argonMem too low (minimum recommended: 64KB)")
	}

	if argonThreads < 1 {
		t.Error("argonThreads too low")
	}

	if keySize != 32 {
		t.Errorf("keySize should be 32 (256 bits), got %d", keySize)
	}

	if saltSize != 16 {
		t.Errorf("saltSize should be 16, got %d", saltSize)
	}

	if nonceSize != 12 {
		t.Errorf("nonceSize should be 12 (96 bits for GCM), got %d", nonceSize)
	}
}

// TestDecryptAuthenticationTagTampering detects tag manipulation
func TestDecryptAuthenticationTagTampering(t *testing.T) {
	plaintext := []byte("sensitive data requiring authentication")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// GCM appends auth tag at end - flip bits in last few bytes
	if len(ciphertext) > 5 {
		corrupted := make([]byte, len(ciphertext))
		copy(corrupted, ciphertext)
		corrupted[len(corrupted)-1] ^= 0xFF // Corrupt tag

		_, err = Decrypt(corrupted, masterKey)
		if err == nil {
			t.Fatal("Should reject corrupted authentication tag")
		}
	}
}

// TestMultipleEncryptsDifferent ensures each encryption is unique
func TestMultipleEncryptsDifferent(t *testing.T) {
	plaintext := []byte("same plaintext every time")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertexts := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		ct, err := Encrypt(plaintext, masterKey)
		if err != nil {
			t.Fatalf("Encrypt %d failed: %v", i, err)
		}
		ciphertexts[i] = ct
	}

	// All ciphertexts should be different (due to random salt/nonce)
	for i := 0; i < len(ciphertexts)-1; i++ {
		for j := i + 1; j < len(ciphertexts); j++ {
			if bytes.Equal(ciphertexts[i], ciphertexts[j]) {
				t.Errorf("Ciphertexts %d and %d are identical (should be random)", i, j)
			}
		}
	}
}

// TestDecryptPartialCiphertext rejects incomplete GCM output
func TestDecryptPartialCiphertext(t *testing.T) {
	plaintext := []byte("test")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, _ := Encrypt(plaintext, masterKey)

	// Remove last byte (GCM tag)
	if len(ciphertext) > 1 {
		incomplete := ciphertext[:len(ciphertext)-1]
		_, err := Decrypt(incomplete, masterKey)
		if err == nil {
			t.Fatal("Should reject incomplete GCM tag")
		}
	}
}

// BenchmarkEncrypt measures encryption performance
func BenchmarkEncrypt(b *testing.B) {
	plaintext := []byte("test plaintext data")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Encrypt(plaintext, masterKey)
	}
}

// BenchmarkDecrypt measures decryption performance
func BenchmarkDecrypt(b *testing.B) {
	plaintext := []byte("test plaintext data")
	masterKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	ciphertext, _ := Encrypt(plaintext, masterKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Decrypt(ciphertext, masterKey)
	}
}

// BenchmarkDeriveKey measures key derivation performance
func BenchmarkDeriveKey(b *testing.B) {
	masterKey := "test-master-key"
	salt := make([]byte, saltSize)
	rand.Read(salt)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = deriveKey(masterKey, salt)
	}
}
