# Provider System & Secret Retrieval Review

**Date:** 2026-07-20  
**Scope:** Provider interface, implementations, and error handling

---

## Provider Interface

**Expected location**: `pkg/provider/*.go`

**Likely interface** (inferred from config):
```go
type Provider interface {
  GetSecret(ctx context.Context, path string) (string, error)
  SetSecret(ctx context.Context, path string, value string) error
  ListSecrets(ctx context.Context, prefix string) ([]string, error)
  DeleteSecret(ctx context.Context, path string) error
}
```

**Required methods** (from spec):
1. **GetSecret** - Retrieve secret by path
2. **SetSecret** - Store secret value
3. **ListSecrets** - Enumerate secrets (for discovery)
4. **DeleteSecret** - Remove secret (for cleanup)

---

## Implemented Providers

### 1. AWS Secrets Manager
- **Type**: `aws`
- **Auth methods**:
  - IAM role (EC2 instance role)
  - Access key + secret
  - Environment variables
- **Region handling**: From config or AWS_REGION env
- **Caching**: Agent-level in-memory cache
- **Retry logic**: Configurable attempts + exponential backoff
- **Error handling**: Network timeouts, auth failures, secret not found

**Configuration**:
```yaml
providers:
  aws:
    type: aws
    region: us-east-1
    auth:
      method: iam_role
    retry:
      attempts: 3
      backoff: 1s
```

### 2. Azure Key Vault
- **Type**: `azure`
- **Auth methods**:
  - Managed identity (MSI)
  - Service principal
  - Access key
- **Vault URL**: From config (e.g., https://myvault.vault.azure.net/)
- **Caching**: Agent-level in-memory cache
- **Retry logic**: Configurable attempts + backoff
- **Error handling**: Network, auth, not found

**Configuration**:
```yaml
providers:
  azure:
    type: azure
    auth:
      method: msi
    config:
      vault_url: https://myvault.vault.azure.net/
```

### 3. HashiCorp Vault
- **Type**: `vault`
- **Auth methods**:
  - Token auth
  - AppRole
  - Kubernetes auth
- **Address**: From config or VAULT_ADDR
- **Token**: From VAULT_TOKEN or config
- **Caching**: Agent-level in-memory cache
- **Retry logic**: Configurable attempts + backoff
- **Error handling**: Connectivity, auth, secret not found

**Configuration**:
```yaml
providers:
  vault:
    type: vault
    auth:
      method: token
    config:
      address: https://vault.example.com:8200
      token: "${VAULT_TOKEN}"
```

### 4. Local Vault
- **Type**: `local`
- **Encryption**: AES-256-GCM
- **Master key storage**: ~/.dso/master.key (user mode) or /etc/dso/master.key (agent)
- **Vault file**: ~/.dso/vault.enc (user mode) or /var/lib/dso/vault.enc (agent)
- **Caching**: Agent-level in-memory cache
- **Key derivation**: PBKDF2 or similar (from master key)
- **No network**: All operations local

**Configuration**:
```yaml
providers:
  local:
    type: local
    config:
      vault_file: ~/.dso/vault.enc
      master_key_file: ~/.dso/master.key
```

---

## Provider Selection Logic

**Configuration mapping**:
```yaml
secrets:
  - name: database_credentials
    provider: aws              # Must match providers.[name]
    mappings:
      MYSQL_PASSWORD: prod/mysql/password  # Secret path in AWS
```

**Runtime lookup**:
1. Secret specifies `provider: aws`
2. Agent looks up `Config.Providers["aws"]`
3. Creates/retrieves provider instance
4. Calls `provider.GetSecret("prod/mysql/password")`
5. Returns value to injector

**Error if**:
- Provider name not found in config
- Provider type unknown
- Provider initialization fails
- Secret path not found

---

## Error Handling

### Network Failures
- **Timeout**: Configurable per provider (default 30s)
- **Retry strategy**: Exponential backoff (1s → max backoff)
- **Max retries**: Configurable (AWS: 3 default)
- **Circuit breaker**: None detected (no fail-fast)

### Authentication Failures
- **Invalid credentials**: Error immediately (no retry)
- **Token expired**: May retry with refresh (depends on provider)
- **IAM role unavailable**: Retry with backoff (EC2 metadata service)
- **Fallback**: None (fail hard)

### Secret Not Found
- **Behavior**: Return error (not empty string)
- **Agent response**: Inject empty value? Skip injection? Fail rotation?
- **Retry**: No (secret not existing won't change on retry)

### Provider-Specific Errors

**AWS**:
- ResourceNotFoundException → Secret doesn't exist
- InvalidParameterException → Bad path format
- AccessDeniedException → IAM permissions issue
- DecryptionFailureException → Encryption issue

**Azure**:
- Unauthorized → Auth failed
- ResourceNotFound → Secret doesn't exist
- RequestFailed → Network or service error

**Vault**:
- Unauthorized → Auth failed or token expired
- NotFound → Secret path doesn't exist
- Unavailable → Vault server unreachable

**Local**:
- FileNotFound → vault.enc missing
- DecryptionError → Wrong master key or corrupted file
- PermissionDenied → Can't read vault file

---

## Caching Strategy

### Agent Cache
- **Storage**: In-memory map (thread-safe)
- **TTL**: No TTL mentioned (cached forever?)
- **Invalidation**: Only on explicit secret change
- **Size limit**: Unbounded (memory risk?)
- **Persistence**: Lost on restart

### Polling for Changes
- **Interval**: Configurable (default 30s)
- **Adaptive backoff**: Reduces polling when no changes
- **Max interval**: 5 minutes (mentioned in agent code)
- **Trigger**: Secret change detected → Rotation initiated

### Webhook Alternative
- **Supported**: Yes (WebhookConfig in agent)
- **Enabled**: Optional in config
- **Endpoint**: DSO exposes webhook receiver
- **Auth**: Token-based protection

---

## Gaps/Issues Found

### Critical Issues:

1. **❌ No distributed provider caching**
   - Multiple DSO agents can't share cache
   - Each agent independently polls providers
   - Inefficient for high-secret-count setups
   - No cache invalidation protocol

2. **❌ Circuit breaker not implemented**
   - Failed provider calls retry indefinitely
   - Can cause cascading failures
   - No fail-fast for obviously broken providers

3. **❌ Cache TTL not configurable**
   - Cache lives forever (or until restart)
   - Stale secrets if provider updates are missed
   - No way to force refresh

### Medium Issues:

4. **❓ Master key security**
   - Master key stored as plaintext file (~/.dso/master.key)
   - Risk if home directory world-readable
   - No key rotation mechanism
   - No HSM/KMS support

5. **❓ Credential storage in config**
   - VAULT_TOKEN in yaml (even with env var support)
   - AWS access keys in config (should use IAM role)
   - No secret scanning for credentials in config

6. **⚠️ Error handling inconsistency**
   - Different providers may fail differently
   - No unified error types
   - Hard to distinguish "auth failed" from "secret not found"

### Low Issues:

7. **❓ Provider plugin architecture**
   - Plugins in separate binaries (`cmd/plugins/`)
   - No dynamic loading documented
   - Plugin discovery mechanism unclear
   - Communication protocol between agent and plugins?

8. **❓ Retry strategy inconsistency**
   - Different retry configs per provider
   - No standard retry envelope
   - Backoff calculations may differ

---

## Testing Gaps

### What Should Be Tested:
1. ✓ Each provider type (AWS/Azure/Vault/Local)
2. ✓ Auth method for each provider
3. ✓ Secret retrieval success path
4. ✓ Secret not found error
5. ✓ Network timeout and retry
6. ✓ Auth failure (wrong credentials)
7. ✓ Cache hit and miss scenarios
8. ✓ Cache invalidation on secret update
9. ✓ Concurrent provider access
10. ✓ Provider initialization failures

### Integration Tests Needed:
- Real AWS/Azure/Vault instances (or mocked)
- Full rotation cycle with each provider
- Failover between providers

---

## Recommendations

1. **Implement circuit breaker** - Fail fast after N consecutive failures
2. **Add cache TTL** - Make secret cache TTL configurable (default 5 min)
3. **Standardize error types** - Create provider-agnostic error types
4. **Secure master key** - Store in /run (tmpfs) not disk, or use KMS
5. **Add credential scanning** - Prevent plaintext secrets in config
6. **Document plugin protocol** - How do plugins communicate with agent?
7. **Add distributed cache** - Share cache across agents (Redis/memcached)
8. **Implement metrics** - Track cache hits, misses, provider latency
9. **Add provider health checks** - Periodic validation of connectivity/auth
10. **Support key rotation** - Mechanism to rotate master key for local vault
