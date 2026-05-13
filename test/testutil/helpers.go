// Package testutil provides common test utilities and helpers for DSO testing.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/vault"
)

// TempVault creates an isolated temporary vault for testing
type TempVault struct {
	HomeDir   string
	VaultPath string
	Vault     *vault.Vault
	t         testing.TB
}

// NewTempVault creates a new test vault in a temporary directory
func NewTempVault(t testing.TB) *TempVault {
	tmpDir := t.TempDir()

	// Set up temporary home directory
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)

	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
	})

	// Initialize vault
	if err := vault.InitDefault(); err != nil {
		t.Fatalf("Failed to initialize test vault: %v", err)
	}

	// Load vault
	v, err := vault.LoadDefault()
	if err != nil {
		t.Fatalf("Failed to load test vault: %v", err)
	}

	vaultDir := filepath.Join(tmpDir, ".dso")

	return &TempVault{
		HomeDir:   tmpDir,
		VaultPath: filepath.Join(vaultDir, "vault.enc"),
		Vault:     v,
		t:         t,
	}
}

// SetSecret stores a secret in the test vault
func (tv *TempVault) SetSecret(project, path, value string) error {
	return tv.Vault.Set(project, path, value)
}

// GetSecret retrieves a secret from the test vault
func (tv *TempVault) GetSecret(project, path string) (string, error) {
	secret, err := tv.Vault.Get(project, path)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}

// SetSecrets stores multiple secrets
func (tv *TempVault) SetSecrets(project string, secrets map[string]string) error {
	return tv.Vault.SetBatch(project, secrets)
}

// ListSecrets returns all secrets for a project
func (tv *TempVault) ListSecrets(project string) ([]string, error) {
	return tv.Vault.List(project)
}

// Close cleans up the test vault
func (tv *TempVault) Close() error {
	// Cleanup handled by t.TempDir() and t.Cleanup()
	return nil
}

// MockProvider is a test mock for the provider system
type MockProvider struct {
	Secrets map[string]string
	Fail    bool
}

// GetSecret returns a mock secret
func (mp *MockProvider) GetSecret(path string) (string, error) {
	if mp.Fail {
		return "", fmt.Errorf("mock provider error")
	}
	secret, ok := mp.Secrets[path]
	if !ok {
		return "", fmt.Errorf("secret not found: %s", path)
	}
	return secret, nil
}

// PutSecret stores a secret in the mock
func (mp *MockProvider) PutSecret(path, value string) error {
	if mp.Fail {
		return fmt.Errorf("mock provider error")
	}
	mp.Secrets[path] = value
	return nil
}

// NewMockProvider creates a new mock provider
func NewMockProvider() *MockProvider {
	return &MockProvider{
		Secrets: make(map[string]string),
		Fail:    false,
	}
}

// DockerTestHelper provides Docker-related test utilities
type DockerTestHelper struct {
	t         testing.TB
	HasDocker bool
}

// NewDockerTestHelper creates a new Docker test helper
func NewDockerTestHelper(t testing.TB) *DockerTestHelper {
	// Check if Docker is available
	_, err := os.Stat("/var/run/docker.sock")
	hasDocker := err == nil

	return &DockerTestHelper{
		t:         t,
		HasDocker: hasDocker,
	}
}

// SkipIfNoDocker skips test if Docker is unavailable
func (dth *DockerTestHelper) SkipIfNoDocker() {
	if !dth.HasDocker {
		dth.t.Skip("Docker not available")
	}
}

// IsDockerAvailable returns true if Docker is available
func (dth *DockerTestHelper) IsDockerAvailable() bool {
	return dth.HasDocker
}

// TestSecretValues provides common test secret values
var TestSecretValues = map[string]string{
	"simple":           "mysecret123",
	"withSpecialChars": "p@$$w0rd!#%&*()[]{}~`^",
	"withUnicode":      "パスワード密碼🔐",
	"empty":            "",
	"long":             string(make([]byte, 10000)), // 10KB secret
	"base64":           "dXNlcm5hbWU6cGFzc3dvcmQ=",
	"json":             `{"username":"admin","password":"secret"}`,
	"multiline":        "line1\nline2\nline3",
	"withWhitespace":   "  secret with spaces  ",
	"numeric":          "1234567890",
}

// AssertSecretEqual asserts two secrets are equal
func AssertSecretEqual(t testing.TB, expected, actual string) {
	if expected != actual {
		t.Errorf("Secret mismatch.\nExpected: %q\nActual:   %q", expected, actual)
	}
}

// AssertErrorNil asserts error is nil
func AssertErrorNil(t testing.TB, err error, msg string) {
	if err != nil {
		t.Errorf("%s: %v", msg, err)
	}
}

// AssertErrorNotNil asserts error is not nil
func AssertErrorNotNil(t testing.TB, err error, msg string) {
	if err == nil {
		t.Errorf("%s: expected error but got nil", msg)
	}
}

// FileTestHelper provides file-related test utilities
type FileTestHelper struct {
	t       testing.TB
	TempDir string
}

// NewFileTestHelper creates a new file test helper
func NewFileTestHelper(t testing.TB) *FileTestHelper {
	return &FileTestHelper{
		t:       t,
		TempDir: t.TempDir(),
	}
}

// WriteFile writes content to a file
func (fth *FileTestHelper) WriteFile(relPath, content string) string {
	fullPath := filepath.Join(fth.TempDir, relPath)

	// Create directory if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fth.t.Fatalf("Failed to create directory %s: %v", dir, err)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		fth.t.Fatalf("Failed to write file %s: %v", fullPath, err)
	}

	return fullPath
}

// ReadFile reads content from a file
func (fth *FileTestHelper) ReadFile(relPath string) string {
	fullPath := filepath.Join(fth.TempDir, relPath)

	content, err := os.ReadFile(fullPath)
	if err != nil {
		fth.t.Fatalf("Failed to read file %s: %v", fullPath, err)
	}

	return string(content)
}

// AssertFileExists asserts a file exists
func (fth *FileTestHelper) AssertFileExists(relPath string) {
	fullPath := filepath.Join(fth.TempDir, relPath)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		fth.t.Errorf("Expected file to exist: %s", fullPath)
	}
}

// AssertFileNotExists asserts a file does not exist
func (fth *FileTestHelper) AssertFileNotExists(relPath string) {
	fullPath := filepath.Join(fth.TempDir, relPath)

	if _, err := os.Stat(fullPath); err == nil {
		fth.t.Errorf("Expected file not to exist: %s", fullPath)
	}
}

// AssertFilePermissions asserts file has correct permissions
func (fth *FileTestHelper) AssertFilePermissions(relPath string, expectedPerm os.FileMode) {
	fullPath := filepath.Join(fth.TempDir, relPath)

	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		fth.t.Fatalf("Failed to stat file %s: %v", fullPath, err)
	}

	if fileInfo.Mode().Perm() != expectedPerm {
		fth.t.Errorf("File permissions mismatch for %s.\nExpected: %#o\nActual:   %#o",
			fullPath, expectedPerm, fileInfo.Mode().Perm())
	}
}

// ConcurrencyTestHelper helps test concurrent operations
type ConcurrencyTestHelper struct {
	t testing.TB
}

// NewConcurrencyTestHelper creates a new concurrency test helper
func NewConcurrencyTestHelper(t testing.TB) *ConcurrencyTestHelper {
	return &ConcurrencyTestHelper{t: t}
}

// RunConcurrent runs n goroutines executing fn concurrently
func (cth *ConcurrencyTestHelper) RunConcurrent(n int, fn func(int) error) {
	done := make(chan error, n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			done <- fn(idx)
		}(i)
	}

	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			cth.t.Errorf("Concurrent operation %d failed: %v", i, err)
		}
	}
}

// RunConcurrentWithBarrier runs n goroutines with a start barrier
func (cth *ConcurrencyTestHelper) RunConcurrentWithBarrier(n int, fn func(int) error) {
	ready := make(chan struct{})
	done := make(chan error, n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			<-ready // Wait for signal to start
			done <- fn(idx)
		}(i)
	}

	// Signal all goroutines to start simultaneously
	close(ready)

	// Wait for all to complete
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			cth.t.Errorf("Concurrent operation %d failed: %v", i, err)
		}
	}
}

// RetryHelper provides retry logic for flaky operations
type RetryHelper struct {
	t        testing.TB
	maxRetry int
}

// NewRetryHelper creates a retry helper with default 3 retries
func NewRetryHelper(t testing.TB) *RetryHelper {
	return &RetryHelper{t: t, maxRetry: 3}
}

// WithRetries sets max retry count
func (rh *RetryHelper) WithRetries(n int) *RetryHelper {
	rh.maxRetry = n
	return rh
}

// Retry executes fn with exponential backoff, retrying on error
func (rh *RetryHelper) Retry(fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < rh.maxRetry; attempt++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}
