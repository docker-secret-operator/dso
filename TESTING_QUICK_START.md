# DSO Testing Quick Start Guide

**Quick Links:** 
- Full roadmap: [`TESTING_IMPLEMENTATION_ROADMAP.md`](TESTING_IMPLEMENTATION_ROADMAP.md)
- Gap analysis: [`COVERAGE_GAP_ANALYSIS.md`](COVERAGE_GAP_ANALYSIS.md)
- Phase 1 summary: [`PHASE_1_IMPLEMENTATION_SUMMARY.md`](PHASE_1_IMPLEMENTATION_SUMMARY.md)

---

## Running Tests

### Basic Commands

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with race detector (detects data races)
go test -race ./...

# Run with coverage report
go test -cover ./...

# Generate HTML coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Package-Specific Tests

```bash
# Vault tests (crypto, encryption, persistence)
go test -v ./pkg/vault/...

# Compose AST tests (YAML parsing, tmpfs injection)
go test -v ./internal/compose/...

# Config tests (configuration loading and validation)
go test -v ./pkg/config/...

# Run specific test
go test -v -run TestVaultEncryptDecryptRoundtrip ./pkg/vault/...
```

### Benchmarks

```bash
# Run benchmarks
go test -bench=. ./pkg/vault/...

# With memory stats
go test -bench=. -benchmem ./pkg/vault/...

# With CPU profiling
go test -bench=. -cpuprofile=cpu.prof ./pkg/vault/...
```

### Advanced

```bash
# Run tests with custom timeout
go test -timeout 5m ./...

# Count runs (avoid caching)
go test -count=1 ./...

# Fail fast on first failure
go test -failfast ./...

# Verbose with race and coverage
go test -v -race -cover ./...
```

---

## Test Structure

### Phase 1: Complete ✅

| Package | Tests | Coverage | Status |
|---------|-------|----------|--------|
| `pkg/vault` | 17 | 85%+ | ✅ Done |
| `pkg/vault/crypto` | 20 | 90%+ | ✅ Done |
| `internal/compose` | 18 | 85%+ | ✅ Done |
| `pkg/config` | 15 | 70%+ | ✅ Done |

**Total:** 70 tests, 1500+ LOC

### Phase 2-5: Planned

| Phase | Focus | Tests | ETA |
|-------|-------|-------|-----|
| Phase 2 | Injector, Resolver, Providers | ~37 | Week 2-3 |
| Phase 3 | CLI & Integration | ~40 | Week 3-4 |
| Phase 4 | Security & Performance | ~70 | Week 4-5 |
| Phase 5 | CI/CD & Polish | ~30 | Week 5-6 |

---

## Test Files Reference

### Phase 1 (Created)

1. **`pkg/vault/vault_test.go`** (450+ LOC)
   - Vault initialization and loading
   - Secret set/get/list operations
   - Persistence and recovery
   - Metadata tracking
   - Concurrent access
   - Error handling

2. **`pkg/vault/crypto_test.go`** (400+ LOC)
   - Encryption/decryption roundtrips
   - Key derivation (Argon2id)
   - Ciphertext format validation
   - Corruption detection
   - Unicode handling
   - Performance benchmarks

3. **`internal/compose/ast_test.go`** (350+ LOC)
   - YAML AST node operations
   - Map key/value manipulation
   - tmpfs mount injection
   - UID/GID parsing
   - Type safety and nil handling
   - Structure preservation

4. **`pkg/config/config_test.go`** (Enhanced)
   - Configuration loading (v1/v2)
   - YAML validation
   - Path security
   - Environment overrides
   - Default values
   - Migration compatibility

5. **`test/testutil/helpers.go`** (300+ LOC)
   - `TempVault` - Temporary vault helper
   - `MockProvider` - Mock provider for testing
   - `DockerTestHelper` - Docker availability detection
   - `FileTestHelper` - File operation utilities
   - `ConcurrencyTestHelper` - Concurrent test execution
   - Test secret values and assertions

---

## Key Test Functions

### Vault Tests

**Security-Critical:**
- `TestEncryptDecryptRoundtrip` - Core encryption validation
- `TestDecryptWithWrongKey` - Key validation
- `TestVaultChecksumValidation` - Tamper detection
- `TestVaultConcurrentAccess` - Race condition detection
- `TestMasterKeyGeneration` - Secure random generation

**Functionality:**
- `TestVaultSetAndGet` - Basic operations
- `TestVaultList` - List all secrets
- `TestVaultPersistence` - Data persistence
- `TestVaultSetBatch` - Batch operations

**Input Validation:**
- `TestVaultSetInvalidProject` - Path traversal prevention
- `TestDecryptTruncatedCiphertext` - Malformed input handling

### Compose Tests

**Safety:**
- `TestGetMapValueHandlesNilNode` - Nil safety
- `TestSetMapValueHandlesNonMappingNode` - Type safety
- `TestAddTmpfsMountPreservesOtherFields` - Non-corruption

**Functionality:**
- `TestAddTmpfsMountCreatesMount` - Mount creation
- `TestAddTmpfsMountDeduplicatesMounts` - Duplicate prevention
- `TestExtractUIDGIDFromString` - UID/GID parsing

**Edge Cases:**
- `TestGetMapValueWithSpecialCharacters` - Special char keys
- `TestComposeASTComplexStructure` - Nested structures

### Config Tests

- `TestLoadConfigV1` - Legacy v1 format
- `TestLoadConfigV2` - Modern v2 format
- `TestLoadConfigInvalidYAML` - Error handling
- `TestIsSafePathRejectsPrefixSibling` - Security validation
- `TestConfigValidation` - Validation logic

---

## Using Test Helpers

### Creating a Test Vault

```go
func TestMyFeature(t *testing.T) {
    // Create isolated temporary vault
    tv := testutil.NewTempVault(t)
    
    // Set secrets
    tv.SetSecret("myapp", "db_password", "secret123")
    
    // Get secrets
    value, err := tv.GetSecret("myapp", "db_password")
    
    // List secrets
    secrets, err := tv.ListSecrets("myapp")
    
    // Automatic cleanup via t.Cleanup()
}
```

### Using Mock Provider

```go
func TestProviderIntegration(t *testing.T) {
    mp := testutil.NewMockProvider()
    mp.PutSecret("path/secret", "value")
    
    value, err := mp.GetSecret("path/secret")
    
    // Simulate failure
    mp.Fail = true
    _, err = mp.GetSecret("path/secret")
}
```

### File Operations

```go
func TestFileHandling(t *testing.T) {
    fth := testutil.NewFileTestHelper(t)
    
    // Write file
    path := fth.WriteFile("config.yaml", "content")
    
    // Read file
    content := fth.ReadFile("config.yaml")
    
    // Assertions
    fth.AssertFileExists("config.yaml")
    fth.AssertFilePermissions("config.yaml", 0644)
}
```

### Concurrent Testing

```go
func TestConcurrentOps(t *testing.T) {
    cth := testutil.NewConcurrencyTestHelper(t)
    
    // Run 10 concurrent operations
    cth.RunConcurrent(10, func(idx int) error {
        return vault.Set("app", fmt.Sprintf("secret-%d", idx), "value")
    })
}
```

---

## Common Test Patterns

### Table-Driven Tests

```go
func TestEncryptDecrypt(t *testing.T) {
    tests := []struct {
        name      string
        plaintext []byte
        masterKey string
    }{
        {"simple", []byte("secret"), "key123"},
        {"unicode", []byte("パスワード"), "key123"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Assertion Helpers

```go
// Use helper functions
testutil.AssertSecretEqual(t, expected, actual)
testutil.AssertErrorNil(t, err, "operation failed")
testutil.AssertErrorNotNil(t, err, "expected error")
```

### Resource Cleanup

```go
func TestWithCleanup(t *testing.T) {
    tv := testutil.NewTempVault(t)
    
    // Cleanup automatically called via t.Cleanup()
    // Safe to test with -race flag
}
```

---

## Common Issues & Solutions

### Test Hangs/Timeout

**Problem:** Test doesn't complete  
**Solution:**
```bash
go test -timeout 30s ./...  # Set explicit timeout
```

### Race Condition Detected

**Problem:** Data race warning with `-race`  
**Solution:**
- Check for concurrent map access
- Use `sync.RWMutex` for vault
- Run: `go test -race ./...` to detect

### Flaky Tests

**Problem:** Test fails intermittently  
**Solution:**
- Avoid time-dependent assertions
- Use barriers for concurrent tests
- Check for file system race conditions

### Test Caching Issues

**Problem:** Test passes but shouldn't  
**Solution:**
```bash
go test -count=1 ./...  # Bypass cache
go clean -testcache  # Clear cache
```

---

## CI/CD Integration

### GitHub Actions

Current workflow runs:
```bash
go test -v -race -count=1 ./...
```

To run locally same as CI:
```bash
go test -v -race -count=1 ./...
```

### Pre-commit Hook

Create `.git/hooks/pre-commit`:
```bash
#!/bin/bash
go test -race ./... || exit 1
```

### IDE Integration

**VS Code:** Install Go extension, tests run in integrated terminal  
**GoLand/IntelliJ:** Right-click test file → Run  
**Command Line:** See basic commands above

---

## Performance Baselines

Run benchmarks to establish performance:

```bash
# Encryption performance
go test -bench=BenchmarkEncrypt -benchmem ./pkg/vault

# Decryption performance  
go test -bench=BenchmarkDecrypt -benchmem ./pkg/vault

# Key derivation
go test -bench=BenchmarkDeriveKey -benchmem ./pkg/vault
```

Expected results:
- Encrypt: <5ms per operation
- Decrypt: <5ms per operation
- DeriveKey: <100ms (Argon2id is intentionally slow)

---

## Writing New Tests

### Minimal Test Template

```go
package mypackage

import "testing"

func TestMyFeature(t *testing.T) {
    // Setup
    
    // Execute
    result, err := MyFunction()
    
    // Verify
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

### Using TestHelpers

```go
func TestWithVault(t *testing.T) {
    tv := testutil.NewTempVault(t)
    
    // Your test using tv.SetSecret, tv.GetSecret, etc.
}
```

### Security Test Template

```go
func TestSecurityProperty(t *testing.T) {
    // Verify security guarantee
    // e.g., secret not in logs, file permissions correct
    
    // Use assertions
    testutil.AssertErrorNil(t, err, "setup failed")
}
```

---

## Code Coverage Goals

### By Phase

| Phase | Coverage | Status |
|-------|----------|--------|
| Phase 1 | 30% | ✅ Done |
| Phase 2 | 50% | ⏳ In Progress |
| Phase 3 | 65% | ⏳ Planned |
| Phase 4 | 80% | ⏳ Planned |
| Phase 5 | 85%+ | ⏳ Planned |

### By Package (Phase 1)

| Package | Coverage |
|---------|----------|
| `pkg/vault` | 85%+ |
| `pkg/vault/crypto` | 90%+ |
| `internal/compose` | 85%+ |
| `pkg/config` | 70%+ |

---

## Troubleshooting

### Import Errors

```bash
# If tests don't compile due to imports
go mod tidy          # Update dependencies
go mod download      # Download dependencies
```

### Module Not Found

```bash
# If test helpers not found
go mod edit -require=github.com/docker-secret-operator/dso@v3.2.0
```

### Permission Errors

```bash
# On Linux, may need Docker socket permission
sudo usermod -aG docker $USER
```

---

## Next Steps

1. **Run Phase 1 tests**
   ```bash
   go test -v -race -cover ./pkg/vault/... ./internal/compose/... ./pkg/config/...
   ```

2. **Review test coverage**
   ```bash
   go test -coverprofile=coverage.out ./...
   go tool cover -html=coverage.out
   ```

3. **Check for races**
   ```bash
   go test -race ./...
   ```

4. **Plan Phase 2**
   - Review `TESTING_IMPLEMENTATION_ROADMAP.md`
   - Identify injector tests to implement
   - Create provider mocks

5. **Contribute**
   - All new PRs should include tests
   - Maintain or improve coverage %
   - Use test helpers from `test/testutil/`

---

## Resources

- **Testing Guide:** `TESTING_IMPLEMENTATION_ROADMAP.md`
- **Gap Analysis:** `COVERAGE_GAP_ANALYSIS.md`
- **Phase 1 Summary:** `PHASE_1_IMPLEMENTATION_SUMMARY.md`
- **Test Helpers:** `test/testutil/helpers.go`
- **Go Testing Docs:** https://pkg.go.dev/testing

---

**Happy Testing! 🧪**

For questions or issues, refer to the full documentation files or review the test code directly.
