package providers

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// PluginVerifier ensures plugin binaries are legitimate and haven't been tampered with
type PluginVerifier struct {
	logger        *zap.Logger
	mu            sync.RWMutex
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

	pv.mu.Lock()
	pv.trustedHashes[pluginName] = hash
	pv.mu.Unlock()
	pv.logger.Debug("Registered trusted plugin hash",
		zap.String("plugin", pluginName),
		zap.String("hash", hash[:16]+"..."))

	return nil
}

// LoadTrustedHashesFromFile loads plugin hashes from a manifest file
// File format: one entry per line: "plugin_name=sha256_hash"
func (pv *PluginVerifier) LoadTrustedHashesFromFile(manifestPath string) error {
	f, err := os.Open(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024), 1024)

	// Track names seen in this load to detect duplicates within the file.
	seen := make(map[string]struct{})

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			pv.logger.Warn("Invalid manifest line", zap.String("line", line))
			continue
		}

		name := strings.TrimSpace(line[:idx])
		hash := strings.TrimSpace(line[idx+1:])

		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate manifest entry for plugin %q", name)
		}
		seen[name] = struct{}{}

		if err := pv.RegisterTrustedHash(name, hash); err != nil {
			pv.logger.Warn("Failed to register hash from manifest",
				zap.String("plugin", name),
				zap.Error(err))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading manifest: %w", err)
	}

	return nil
}

// VerifyPluginBinary verifies a plugin binary against its registered hash
func (pv *PluginVerifier) VerifyPluginBinary(pluginPath string) error {
	pluginName := filepath.Base(pluginPath)

	// Check if hash is registered
	pv.mu.RLock()
	expectedHash, registered := pv.trustedHashes[pluginName]
	pv.mu.RUnlock()
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

	// Verify hash matches using constant-time comparison to avoid timing oracles.
	if subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) != 1 {
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
