package providers

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// PluginVerifier ensures plugin binaries are legitimate and haven't been tampered with
type PluginVerifier struct {
	logger        *zap.Logger
	trustedHashes map[string]string // plugin name -> expected SHA256 hash
	allowUnsigned bool              // whether to allow unsigned plugins in dev mode
}

// NewPluginVerifier creates a plugin verifier
func NewPluginVerifier(logger *zap.Logger, allowUnsigned bool) *PluginVerifier {
	return &PluginVerifier{
		logger:        logger,
		trustedHashes: make(map[string]string),
		allowUnsigned: allowUnsigned,
	}
}

// RegisterTrustedHash registers a plugin's expected SHA256 hash
func (pv *PluginVerifier) RegisterTrustedHash(pluginName, hash string) error {
	if len(hash) != 64 {
		return fmt.Errorf("invalid hash length: expected 64 chars, got %d", len(hash))
	}

	// Validate hash is valid hex
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("invalid hex hash: %w", err)
	}

	pv.trustedHashes[pluginName] = hash
	pv.logger.Debug("Registered trusted plugin hash",
		zap.String("plugin", pluginName),
		zap.String("hash", hash[:16]+"..."))

	return nil
}

// LoadTrustedHashesFromFile loads plugin hashes from a manifest file
// File format: one entry per line: "plugin_name=sha256_hash"
func (pv *PluginVerifier) LoadTrustedHashesFromFile(manifestPath string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	// Parse line by line
	lines := make([]string, 0)
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, string(data[start:i]))
			start = i + 1
		}
	}

	for _, line := range lines {
		if len(line) == 0 || line[0] == '#' {
			continue // Skip empty lines and comments
		}

		// Parse "name=hash"
		parts := make([]string, 0)
		partStart := 0
		for i := 0; i < len(line); i++ {
			if line[i] == '=' {
				parts = append(parts, line[partStart:i])
				parts = append(parts, line[i+1:])
				break
			}
		}

		if len(parts) != 2 {
			pv.logger.Warn("Invalid manifest line", zap.String("line", line))
			continue
		}

		name, hash := parts[0], parts[1]
		if err := pv.RegisterTrustedHash(name, hash); err != nil {
			pv.logger.Warn("Failed to register hash from manifest",
				zap.String("plugin", name),
				zap.Error(err))
		}
	}

	return nil
}

// VerifyPluginBinary verifies a plugin binary against its registered hash
func (pv *PluginVerifier) VerifyPluginBinary(pluginPath string) error {
	pluginName := filepath.Base(pluginPath)

	// Check if hash is registered
	expectedHash, registered := pv.trustedHashes[pluginName]
	if !registered {
		if pv.allowUnsigned {
			pv.logger.Warn("Plugin has no registered hash (unsigned)",
				zap.String("plugin", pluginName),
				zap.String("action", "allowing due to dev mode"))
			return nil
		}
		return fmt.Errorf("plugin %s has no registered hash (not allowed)", pluginName)
	}

	// Calculate hash of binary
	actualHash, err := pv.calculateFileHash(pluginPath)
	if err != nil {
		return fmt.Errorf("failed to hash plugin: %w", err)
	}

	// Verify hash matches
	if actualHash != expectedHash {
		pv.logger.Error("Plugin hash mismatch - possible tampering detected",
			zap.String("plugin", pluginName),
			zap.String("expected", expectedHash[:16]+"..."),
			zap.String("actual", actualHash[:16]+"..."))
		return fmt.Errorf("plugin %s hash mismatch: expected %s, got %s",
			pluginName, expectedHash, actualHash)
	}

	pv.logger.Info("Plugin binary verified", zap.String("plugin", pluginName))
	return nil
}

// VerifyPluginSignature verifies a plugin's digital signature (if certificate provided)
func (pv *PluginVerifier) VerifyPluginSignature(pluginPath, signaturePath, certPath string) error {
	// This is a stub for production systems that would use proper code signing
	// Example: using Ed25519 or ECDSA signatures

	_, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("failed to read signature: %w", err)
	}

	certData, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate: %w", err)
	}

	// Parse certificate
	_, err = x509.ParseCertificate(certData)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// In production, would verify signature using cert.PublicKey
	pv.logger.Info("Plugin signature verification skipped",
		zap.String("plugin", pluginPath),
		zap.String("note", "implement using crypto/x509 for production"))

	return nil
}

// calculateFileHash computes SHA256 hash of a file
func (pv *PluginVerifier) calculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// GeneratePluginHash generates hash for a plugin binary (used when creating manifest)
func (pv *PluginVerifier) GeneratePluginHash(pluginPath string) (string, error) {
	hash, err := pv.calculateFileHash(pluginPath)
	if err != nil {
		return "", err
	}
	pv.logger.Info("Generated plugin hash",
		zap.String("plugin", filepath.Base(pluginPath)),
		zap.String("hash", hash))
	return hash, nil
}

// CreateHashManifest creates a manifest file with hashes of all plugins in a directory
func (pv *PluginVerifier) CreateHashManifest(pluginDir, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create manifest: %w", err)
	}
	defer file.Close()

	// Write header
	file.WriteString("# Plugin Hash Manifest\n")
	file.WriteString("# Format: plugin_name=sha256_hash\n\n")

	// List plugins and compute hashes
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return fmt.Errorf("failed to read plugin directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(pluginDir, entry.Name())
		hash, err := pv.calculateFileHash(pluginPath)
		if err != nil {
			pv.logger.Warn("Failed to hash plugin", zap.String("file", entry.Name()), zap.Error(err))
			continue
		}

		line := fmt.Sprintf("%s=%s\n", entry.Name(), hash)
		if _, err := file.WriteString(line); err != nil {
			return fmt.Errorf("failed to write manifest entry: %w", err)
		}

		pv.logger.Debug("Added plugin to manifest",
			zap.String("plugin", entry.Name()),
			zap.String("hash", hash[:16]+"..."))
	}

	pv.logger.Info("Created plugin hash manifest", zap.String("path", outputPath))
	return nil
}
