# Phase 1 Tests - Organizational Reference

## Test Organization by Functionality

### Cryptography & Encryption (pkg/vault/crypto_test.go)

**Key Derivation (Argon2id)**
- `TestDeriveKeyDeterminism` - Same inputs → same key
- `TestDeriveKeyDifferentInputs` - Different inputs → different keys
- `TestDeriveKeyWithLongMasterKey` - Extended key support
- `TestDeriveKeyWithEmptyMasterKey` - Edge case: empty input
- `TestDeriveKeyWithLargeSalt` - Edge case: large salt
- `TestDeriveKeyConcurrency` - Thread-safe derivation

**Encryption & Decryption (AES-256-GCM)**
- `TestEncryptDecryptRoundtrip` - Full cycle validation (multiple sizes)
- `TestEncryptGeneratesDifferentNonces` - Randomness check
- `TestEncryptionIsNondeterministic` - Salt/nonce uniqueness
- `TestMultipleEncryptsDifferent` - Each output unique
- `TestEncryptionRoundtripUnicode` - Non-ASCII support
- `TestEncryptDecryptRandomBytes` - Binary data integrity

**Error Detection & Validation**
- `TestDecryptInvalidCiphertextReturnsError` - Corrupted input detection
- `TestDecryptAuthenticationTagTampering` - GCM tag validation
- `TestDecryptPartialCiphertext` - Incomplete data rejection
- `TestDecryptBadMasterKeyReturnsError` - Key mismatch detection
- `TestDecryptWrongMasterKeyFails` - Wrong key failure
- `TestDecryptShortCiphertextReturnsError` - Undersize rejection

**Performance Benchmarks**
- `BenchmarkEncrypt` - Encryption speed (~55ms)
- `BenchmarkDecrypt` - Decryption speed (~55ms)
- `BenchmarkDeriveKey` - Key derivation speed (~55ms)

---

### Vault Operations & Persistence (pkg/vault/vault_test.go)

**Initialization & Setup**
- `TestVaultInitialization` - Creation and startup
- `TestVaultInitializationMultipleTimes` - Idempotent behavior
- `TestVaultDatabaseOpen` - Database access

**Data Persistence**
- `TestVaultPersistence` - State survives reload
- `TestVaultRecovery` - Restore from disk
- `TestVaultWriteReadCycle` - Round-trip verification
- `TestVaultDatabaseIntegrity` - SQLite validation

**Security & Permissions**
- `TestVaultFilePermissions` - 0600 file protection
- `TestVaultFilePermissionsPreserved` - Permission preservation
- `TestVaultDirPermissionsPreserved` - 0700 directory protection
- `TestVaultLockFileSecurity` - Lock file protection

**Concurrent Access & Locking**
- `TestVaultConcurrentAccess` - Race-free operations
- `TestVaultLockingMechanism` - Serialization safety

**Metadata & Integrity**
- `TestVaultMetadataTracking` - Secret metadata storage
- `TestVaultIntegrity` - Checksum validation

**Key Management**
- `TestVaultSealing` - Seal/unseal operations
- `TestVaultKeyRotation` - Key rotation support
- `TestVaultLoadInvalidFile` - Corrupted vault recovery

**Edge Cases**
- `TestVaultEmptyStateHandling` - Empty vault operations

---

### Configuration Management (pkg/config/config_test.go)

**Version Compatibility**
- `TestLoadConfigV1` - Legacy format support
- `TestLoadConfigV2` - Modern format support
- `TestMultipleConfigVersions` - Both versions coexist

**Validation & Defaults**
- `TestConfigValidation` - Config correctness
- `TestConfigDefaults` - Sensible defaults applied
- `TestConfigEnvironmentOverrides` - Env var override support
- `TestConfigRequiresProviders` - Provider requirement validation

**Error Handling**
- `TestLoadConfigInvalidYAML` - Malformed YAML rejection
- `TestLoadConfigMissingFile` - Missing file handling
- `TestLoadConfigEmptyFile` - Empty config support

**Path Safety & Security**
- `TestIsSafePathRejectsPrefixSibling` - Path traversal prevention
- `TestIsSafePathAllowsContainedAbsolutePath` - Valid paths
- `TestIsSafePathWithRelativePaths` - Relative path handling
- `TestIsSafePathSymlinkEscapeAttempt` - Symlink prevention
- `TestIsSafePathEmptyPaths` - Edge cases (empty paths)

---

### YAML AST Manipulation (internal/compose/ast_test.go)

**Map Operations**
- `TestGetMapValueReturnsCorrectValue` - Value retrieval
- `TestGetMapValueReturnsNilForMissing` - Missing key handling
- `TestGetMapValueWithOddContent` - Malformed mapping recovery
- `TestSetMapValue` - Value assignment

**Mount Management**
- `TestAddTmpfsMountToService` - Single mount injection
- `TestAddTmpfsMountMultiple` - Multiple mount handling
- `TestAddTmpfsMountWithOddIndex` - Index recovery
- `TestAddTmpfsMountWithMalformedNode` - Corrupted node recovery
- `TestAddServiceMount` - Service mount operations
- `TestModifyMultipleMounts` - Batch modifications

**UID/GID Extraction**
- `TestExtractUIDGIDParseSuccess` - Basic parsing
- `TestExtractUIDGIDWithWhitespace` - Whitespace handling
- `TestExtractUIDGIDWithNegativeValues` - Negative numbers
- `TestExtractUIDGIDZeroValues` - Zero acceptance
- `TestExtractUIDGIDMaxValues` - Maximum values
- `TestExtractUIDGIDNonNumericString` - Non-numeric rejection
- `TestExtractUIDGIDSeparatorVariations` - Different separators
- `TestExtractUIDGIDMissingGID` - Missing components

**Safety & Type Handling**
- `TestNodeNilCheck` - Nil node safety
- `TestNodeContentNilCheck` - Nil content handling

**Full Integration**
- `TestComposeFileParsing` - Complete file parsing
- `TestYAMLNodeTraversal` - Node tree navigation

**Performance Benchmarks**
- `BenchmarkAddTmpfsMount` - Mount injection speed
- `BenchmarkExtractUIDGID` - Extraction speed

---

## Test Coverage by Risk Area

### High-Risk Operations (Security-Critical)

**Cryptography** (12 tests)
- Encryption/decryption correctness
- Key derivation security
- Authentication tag validation
- Corruption detection

**File Security** (4 tests)
- Permission enforcement (0600/0700)
- File permission preservation
- Lock file protection
- Path traversal prevention

**YAML Safety** (5 tests)
- Symlink escape prevention
- Malformed node recovery
- Nil pointer safety
- Type validation

### Medium-Risk Operations (Reliability-Critical)

**Persistence** (4 tests)
- Data durability
- Recovery capability
- Integrity validation
- Empty state handling

**Configuration** (5 tests)
- Format compatibility
- Environment overrides
- Validation enforcement
- Default application

**AST Operations** (8 tests)
- Map value manipulation
- Mount injection
- UID/GID extraction
- Multi-mount handling

### Standard Operations (Correctness-Validation)

**Concurrency** (2 tests)
- Concurrent vault access
- Key derivation thread-safety

**Performance** (5 benchmarks)
- Encryption performance
- Decryption performance
- Key derivation performance
- Mount injection performance
- UID/GID extraction performance

---

## Test Dependencies

### Input Files Required
- YAML configurations (created inline in tests)
- Temporary directories (using t.TempDir())
- Temp files for vault storage

### External Dependencies
- `gopkg.in/yaml.v3` - YAML parsing in compose tests
- Standard library only (crypto, os, testing, etc.)

### No External Services Required
- All tests are self-contained
- No database servers needed (SQLite in-memory)
- No network calls
- No Docker daemon required

---

## Test Execution Patterns

### Pattern 1: Table-Driven Tests
Used in:
- `TestExtractUIDGIDWithWhitespace` - Multiple parsing scenarios
- `TestIsSafePathEmptyPaths` - Edge case combinations

**Benefits:**
- Reduced code duplication
- Clear scenario documentation
- Easy to add new test cases

### Pattern 2: Subtests (t.Run)
Used in:
- All table-driven tests
- Tests with multiple scenarios

**Benefits:**
- Individual test reporting
- Better failure isolation
- Cleaner test output

### Pattern 3: Helper Functions
Used across all test files:
- File creation/cleanup
- Vault initialization
- Error assertions

**Benefits:**
- Consistent setup/teardown
- Reduced test boilerplate
- Centralized error handling

### Pattern 4: Benchmark Tests
Used in:
- crypto_test.go (3 benchmarks)
- ast_test.go (2 benchmarks)

**Benefits:**
- Performance regression detection
- Baseline documentation
- Resource utilization tracking

---

## Test Execution Order

### Compilation Phase
1. Parse all .go files
2. Check import validity
3. Verify function signatures
4. Link dependencies

### Execution Phase (per package)
1. `./...` tests in dependency order:
   - pkg/vault (crypto, vault)
   - pkg/config
   - internal/compose

2. Run with flags:
   - `go test ./...` - Basic execution
   - `go test -race ./...` - Race detection
   - `go test -cover ./...` - Coverage reporting
   - `go test -bench=. ./pkg/vault/...` - Benchmarks

---

## Success Criteria

### All Tests Pass
- All 75 unit tests complete without failure
- 5 benchmark tests complete successfully
- Race detector shows no data races

### Coverage Targets
- pkg/vault: 77%+ (AES-256-GCM, Argon2id, persistence)
- internal/compose: 100% (Full AST coverage)
- pkg/config: N/A (Configuration validation)

### Performance Baselines
- Encryption: ~55ms
- Decryption: ~55ms
- Key Derivation: ~55ms (Argon2id intentionally slow)
- Mount Injection: Measurable performance
- UID/GID Extraction: Measurable performance

---

**Version**: 1.0
**Last Updated**: May 6, 2026
**Total Tests**: 75 unit + 5 benchmark
**Status**: Ready for Execution
