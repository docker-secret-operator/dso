# Configuration Loading & Validation Review

**Date:** 2026-07-20  
**Scope:** Config structure, validation rules, schema, and example testing

---

## Config Structure (pkg/config/config.go)

### Main Config Struct
```go
type Config struct {
  Providers map[string]ProviderConfig     // Named provider configurations
  Agent     AgentConfig                   // Agent daemon settings
  Defaults  DefaultsConfig                // Default injection/rotation settings
  Logging   LoggingConfig                 // Log level and format
  Secrets   []SecretMapping               // Array of secrets to manage
  
  // Legacy support
  LegacyProvider string                   // For backward compatibility
  LegacyConfig   map[string]string        // For backward compatibility
}
```

### Sub-Structures

**ProviderConfig**:
- Type: string (aws/azure/vault/local)
- Region: string (AWS-specific)
- Auth: AuthConfig (credentials method)
- Retry: RetryConfig (attempts + backoff)
- Config: map[string]string (provider-specific settings)

**AuthConfig**:
- Method: string (iam_role, access_key, token, env)
- Params: map[string]string (method-specific parameters)

**AgentConfig**:
- Cache: bool (secret caching enabled)
- RefreshInterval: string (polling interval)
- AutoSync: bool (automatic sync mode)
- RestartStrategy: RestartStrategy (container restart behavior)
- Watch: WatchConfig (polling/event/hybrid)
- Rotation: RotationConfigV2

**SecretMapping**:
- Name: string (secret identifier)
- Provider: string (which provider to use)
- Inject: InjectionConfig (env vs file)
- Rotation: RotationConfigV2 (rotation strategy)
- Targets: TargetConfig (which containers)
- Mappings: map[string]string (env vars → secret paths)

**InjectionConfig**:
- Type: string (env or file)
- Path: string (file path for dsofile://)
- UID: int (file owner for dsofile://)
- GID: int (file owner for dsofile://)

**RotationConfigV2**:
- Enabled: bool (rotation enabled)
- Strategy: string (restart, signal, none, rolling)
- Signal: string (SIGHUP, SIGTERM, etc.)
- HealthCheckTimeout: string (duration)

---

## Required vs Optional Fields

| Field | Type | Required | Validation |
|-------|------|----------|------------|
| providers | map[ProviderConfig] | ✅ Yes | At least one provider |
| agent | AgentConfig | ✅ Yes | Watch mode must be valid |
| defaults | DefaultsConfig | ❌ No | Used if secret doesn't specify |
| logging | LoggingConfig | ❌ No | Default: info level |
| secrets | []SecretMapping | ✅ Yes | At least one secret |
| secrets[].name | string | ✅ Yes | Unique identifier |
| secrets[].mappings | map[string]string | ✅ Yes | At least one mapping |
| secrets[].provider | string | ✅ Yes | Must match a configured provider |
| secrets[].inject.type | string | ✅ Yes | env or file |

---

## Validation Rules Applied

### Config-Level Validation (`Config.Validate()`)
```
1. ✅ At least one provider must be configured
2. ✅ Providers must have valid type (aws/azure/vault/local)
3. ✅ Agent config must specify valid watch mode
4. ✅ Logging level must be valid (info/debug/error)
5. ✅ At least one secret must be configured
```

### Secret-Level Validation
```
1. ✅ Secret name is unique
2. ✅ Provider referenced in secret must exist
3. ✅ Injection type must be env or file
4. ✅ Mappings dict has at least one entry
5. ✅ For file injection: path must be specified
6. ✅ Rotation strategy must be valid
7. ✅ For signal rotation: signal must be specified
```

### Provider Validation
```
1. ✅ AWS provider must have region or auth method
2. ✅ Azure provider must have vault URL
3. ✅ Vault provider must have address and auth
4. ✅ Local provider must have vault file path
5. ✅ Retry config must have attempts > 0
6. ✅ Backoff must be valid duration
```

---

## Legacy Format Support

### Format Detection
Config struct has custom `UnmarshalYAML()` to handle:
1. **V2 format** (current): Structured with providers map
2. **V1 format** (legacy): Flat structure with single provider

### V1 Compatibility
```yaml
# V1 (legacy)
provider: aws
region: us-east-1
secrets:
  - name: app_creds
    inject: env                    # String, not struct
    rotation: true                 # Bool, not struct
    mappings:
      DB_PASSWORD: prod/mysql/password
```

### V1 → V2 Conversion
- Top-level `provider` field → Moved to `providers` map
- `provider` value → `providers[name].type`
- `inject: env` → `inject: {type: env}`
- `rotation: true` → `rotation: {enabled: true}`
- `reload_strategy` → `rotation` with strategy field

### SecretMapping Custom Unmarshaler
Handles legacy `inject` as string or bool:
- String: "env" or "file" → InjectionConfig.Type
- Bool: true → RotationConfigV2.Enabled
- Map: "type", "strategy" → RotationConfigV2

---

## JSON Schema Coverage

**Status**: No JSON schema file found in repository

**Expected location**: `pkg/schema/dso.json` (referenced but not found)

**Missing**: Schema validation against JSON schema

**Impact**: 
- No schema validation for tooling (IDE, linters)
- No single source of truth for config format
- Documentation must match code (risk of drift)

---

## Test Cases Against Examples

### Example Files:
- ✅ `examples/dso-local.yaml` - Local vault mode
- ✅ `examples/dso-aws.yaml` - AWS Secrets Manager
- ✅ `examples/dso-azure.yaml` - Azure Key Vault
- ✅ `examples/dso-vault.yaml` - HashiCorp Vault

### Loading Test (Mental):
```go
// Each example should load without error
var cfg config.Config
yaml.Unmarshal(exampleData, &cfg)
if err := cfg.Validate(); err != nil {
    t.Fatalf("Example %s failed validation: %v", filename, err)
}
```

**Predictions**:
- ✅ dso-local.yaml: Should load (local provider)
- ✅ dso-aws.yaml: Should load (AWS provider)
- ✅ dso-azure.yaml: Should load (Azure provider)
- ✅ dso-vault.yaml: Should load (Vault provider)

---

## Gaps/Issues Found

### High Priority Issues:

1. **❌ No JSON Schema** 
   - Expected: `pkg/schema/dso.json` should exist
   - Impact: No schema validation, poor IDE support
   - Recommendation: Generate JSON schema from Go struct

2. **❌ No configuration versioning**
   - Config has no version field to distinguish V1 from V2
   - Risk: Can't validate format properly
   - Recommendation: Add explicit `version: "v2"` field

3. **❌ Insufficient provider auth validation**
   - AWS auth not validated (assumes IAM role/credentials exist)
   - Azure auth not validated (MSI endpoint availability)
   - Vault auth not validated (token/AppRole availability)
   - Risk: Config loads but fails at runtime
   - Recommendation: Add optional validation mode to test auth

### Medium Priority Issues:

4. **❓ Watch mode not fully documented**
   - WatchConfig.Mode supports "polling", "event", "hybrid"
   - Only "polling" is documented in README
   - Recommendation: Document all watch modes

5. **❓ HealthCheckTimeout default unclear**
   - RotationConfigV2 has field but default value unknown
   - Recommendation: Document default (probably 30s)

6. **⚠️ Legacy format deprecation not documented**
   - V1 format still supported but should be deprecated
   - No deprecation warning when loaded
   - Recommendation: Log warning when V1 format detected

### Low Priority Issues:

7. **❓ Config file permissions not validated**
   - No check if /etc/dso/dso.yaml has correct permissions (0664)
   - Risk: Secrets readable by unintended users
   - Recommendation: Validate on load and warn if wrong

8. **❓ Provider config validation is partial**
   - Each provider type has different required fields
   - Validation doesn't check all required fields
   - Risk: Config loads but provider fails to initialize
   - Recommendation: Add per-provider validation rules

---

## Validation Summary

### ✅ Well-Handled:
- Legacy V1 format backward compatibility
- Custom unmarshal for format conversion
- Basic structure validation (required fields)
- Provider existence checking
- Injection type validation

### ❌ Missing/Weak:
- JSON schema for IDE/tooling support
- Per-provider configuration validation
- Authentication testing (does auth method work?)
- File permission validation
- Deprecation warnings for legacy format
- Config version field for clarity

### ⚠️ Needs Improvement:
- Watch mode documentation (only polling documented)
- Health check timeout defaults
- Auth configuration guidance (what are valid params?)
- Example validation (examples should be tested in CI)

---

## Recommendations

1. **Add JSON schema** - Generate from Go struct using tools like json-schema-generator
2. **Add version field** - Make config version explicit (v1 vs v2)
3. **Enhance provider validation** - Test auth credentials during config load (optional mode)
4. **Document all watch modes** - Add polling/event/hybrid to docs
5. **Add deprecation warnings** - Warn when V1 format is detected
6. **Validate file permissions** - Check /etc/dso/dso.yaml permissions on startup
7. **Test all examples in CI** - Ensure examples are valid and load correctly
