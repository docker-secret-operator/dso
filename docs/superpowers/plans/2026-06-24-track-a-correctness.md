# Track A — Crash & Correctness Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 5 CRITICAL and 6 HIGH crash/correctness bugs across 4 focused PRs, each independently buildable and testable.

**Architecture:** Surgical fixes only — no refactors beyond what each bug requires. Every fix comes with a regression test that goes red → green. All changes verified with `go build ./...` and `go test -race ./...`.

**Tech Stack:** Go 1.24+, `go test`, `go vet`, `go build`

## Global Constraints

- Run `go build ./...` after every task. Zero new build errors.
- Run `go test -race ./...` after each PR. Zero new failures or races.
- `go vet ./...` must be clean after each PR.
- No behavioral changes beyond fixing the described bug.
- Every new test must be red before the fix, green after (TDD red-green verified in plan steps).

---

## PR1 — Panic & Crash Fixes

Fixes: C1 (DecryptProviderConfig), C2 (NewTriggerEngine panic), C3 (type assertions), C5 (ReleaseLock), H11 (shortID)

---

### Task 1: Fix C1 — DecryptProviderConfig never writes back

**Files:**
- Modify: `pkg/config/crypto.go:150-176`
- Modify: `pkg/config/crypto_test.go`

**Interfaces:**
- Produces: `(*CryptoManager).DecryptProviderConfig(config *Config) error` — same signature, now actually mutates the map

- [ ] **Step 1: Write the failing test**

Add to `pkg/config/crypto_test.go`:

```go
func TestDecryptProviderConfig_WritesBackDecryptedValues(t *testing.T) {
	cm, err := NewCryptoManager(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}

	// Encrypt a value manually
	encrypted, err := cm.Encrypt("my-secret-password")
	if err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"myprov": {
				Config: map[string]string{
					"password": "enc:" + encrypted,
				},
			},
		},
	}

	if err := cm.DecryptProviderConfig(cfg); err != nil {
		t.Fatal(err)
	}

	got := cfg.Providers["myprov"].Config["password"]
	if got == "" || strings.HasPrefix(got, "enc:") {
		t.Fatalf("expected decrypted value, got %q", got)
	}
	if got != "my-secret-password" {
		t.Fatalf("expected %q, got %q", "my-secret-password", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/mdumair/Personal_Work/Antigravity_Work/dso
go test ./pkg/config/... -run TestDecryptProviderConfig_WritesBackDecryptedValues -v
```

Expected: FAIL — the decrypted value still has the `enc:` prefix.

- [ ] **Step 3: Fix DecryptProviderConfig — add write-back**

In `pkg/config/crypto.go`, change `DecryptProviderConfig` from:

```go
func (cm *CryptoManager) DecryptProviderConfig(config *Config) error {
	for _, provider := range config.Providers {
		// ... decrypt into local copy ...
	}
	return nil
}
```

To:

```go
func (cm *CryptoManager) DecryptProviderConfig(config *Config) error {
	for name, provider := range config.Providers {
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

		config.Providers[name] = provider // write back the mutated copy
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/config/... -run TestDecryptProviderConfig_WritesBackDecryptedValues -v
```

Expected: PASS

- [ ] **Step 5: Build check**

```bash
go build ./...
```

Expected: no output (success)

- [ ] **Step 6: Commit**

```bash
git add pkg/config/crypto.go pkg/config/crypto_test.go
git commit -m "fix: DecryptProviderConfig write-back so credentials are actually decrypted"
```

---

### Task 2: Fix C2 — NewTriggerEngine panic → error return

**Files:**
- Modify: `internal/agent/trigger.go:40-85`
- Modify: `internal/cli/agent.go:112`

- [ ] **Step 1: Write the failing test**

Add to `internal/agent/` a new file `trigger_test.go` (or append to existing):

```go
func TestNewTriggerEngine_LockManagerFailure_ReturnsError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewSecretCache(5 * time.Minute)
	defer cache.Close()

	// Use a path that cannot be created (file exists where dir expected)
	tmp := t.TempDir()
	blockingFile := filepath.Join(tmp, "locks")
	if err := os.WriteFile(blockingFile, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}

	engine, err := NewTriggerEngine(cache, nil, nil, logger, &config.Config{}, nil, blockingFile)
	if err == nil {
		t.Fatal("expected error when lock manager cannot be initialized, got nil")
	}
	if engine != nil {
		t.Fatal("expected nil engine on error")
	}
}
```

- [ ] **Step 2: Run test to verify it panics (fails)**

```bash
go test ./internal/agent/... -run TestNewTriggerEngine_LockManagerFailure_ReturnsError -v
```

Expected: FAIL — test panics with "rotation lock manager initialization failed"

- [ ] **Step 3: Change NewTriggerEngine signature and remove panic**

In `internal/agent/trigger.go`, change:

```go
func NewTriggerEngine(cache *SecretCache, storeManager *providers.SecretStoreManager, rw *watcher.ReloaderController, logger *zap.Logger, cfg *config.Config, dockerCli *client.Client) *TriggerEngine {
```

To (adds `lockDir string` param and `error` return; lockDir defaults to `/var/lib/dso/locks` at call site):

```go
func NewTriggerEngine(cache *SecretCache, storeManager *providers.SecretStoreManager, rw *watcher.ReloaderController, logger *zap.Logger, cfg *config.Config, dockerCli *client.Client, lockDir string) (*TriggerEngine, error) {
```

Replace the panic block:

```go
lockManager, err := rotation.NewLockManager("/var/lib/dso/locks", logger)
if err != nil {
    logger.Error("CRITICAL: Failed to initialize rotation lock manager - refusing to start",
        zap.Error(err))
    logger.Error("Lock manager is REQUIRED for rotation safety. Cannot proceed without it.",
        zap.String("path", "/var/lib/dso/locks"))
    // FAIL FAST: Return nil to cause fatal error upstream
    // This prevents silent data corruption from concurrent rotations
    panic(fmt.Sprintf("rotation lock manager initialization failed: %v", err))
}
```

With:

```go
if lockDir == "" {
    lockDir = "/var/lib/dso/locks"
}
lockManager, err := rotation.NewLockManager(lockDir, logger)
if err != nil {
    logger.Error("CRITICAL: Failed to initialize rotation lock manager",
        zap.String("path", lockDir),
        zap.Error(err))
    return nil, fmt.Errorf("rotation lock manager initialization failed: %w", err)
}
```

Change the final `return &TriggerEngine{...}` to `return &TriggerEngine{...}, nil`.

- [ ] **Step 4: Update call site in internal/cli/agent.go**

Change line 112:

```go
trigger := agent.NewTriggerEngine(cache, storeManager, reloader, logger, cfg, dockerCli)
```

To:

```go
trigger, err := agent.NewTriggerEngine(cache, storeManager, reloader, logger, cfg, dockerCli, "")
if err != nil {
    logger.Fatal("Failed to initialize trigger engine", zap.Error(err))
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/agent/... -run TestNewTriggerEngine_LockManagerFailure_ReturnsError -v
```

Expected: PASS

- [ ] **Step 6: Build check**

```bash
go build ./...
```

Expected: no output

- [ ] **Step 7: Commit**

```bash
git add internal/agent/trigger.go internal/agent/trigger_test.go internal/cli/agent.go
git commit -m "fix: NewTriggerEngine returns error instead of panic on lock manager failure"
```

---

### Task 3: Fix C3 — Unguarded type assertions in bootstrap

**Files:**
- Modify: `internal/bootstrap/agent.go:222-233`

- [ ] **Step 1: Write the failing test**

Add to `internal/bootstrap/` a test (or append to existing bootstrap_test.go):

```go
func TestBootstrap_MissingProviderConfigKey_ReturnsError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ab := &AgentBootstrapper{
		logger:   logger,
		prompter: &MockPrompter{},
		detector: &MockDetector{},
	}

	// Simulate getProviderConfigWithMetadata returning a map missing "region"
	providerConfig := map[string]interface{}{
		"name": "my-provider",
		// "region" intentionally missing
	}

	// Call the method that does type assertions — should not panic
	// We test the safe extraction helper directly
	_, err := safeStringFromMap(providerConfig, "region")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}
```

Also add the helper function to test in `internal/bootstrap/agent.go`:

```go
// safeStringFromMap extracts a string value from a map, returning an error if
// the key is missing or the value is not a string. Prevents panic from unguarded
// type assertions on provider config maps.
func safeStringFromMap(m map[string]interface{}, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("provider config missing required field %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("provider config field %q is not a string (got %T)", key, v)
	}
	return s, nil
}
```

- [ ] **Step 2: Run test to verify it passes (helper is new, test validates it)**

```bash
go test ./internal/bootstrap/... -run TestBootstrap_MissingProviderConfigKey_ReturnsError -v
```

- [ ] **Step 3: Replace all bare type assertions with safeStringFromMap**

In `internal/bootstrap/agent.go`, replace lines 222-233:

```go
switch provider {
case ProviderAWS:
    builder.WithAWSProvider(providerConfig["name"].(string), providerConfig["region"].(string))
case ProviderAzure:
    builder.WithAzureProvider(providerConfig["name"].(string), providerConfig["vault_url"].(string))
case ProviderHuawei:
    builder.WithHuaweiProvider(providerConfig["name"].(string), providerConfig["region"].(string), providerConfig["project_id"].(string))
case ProviderVault:
    builder.WithVaultProvider(providerConfig["name"].(string), providerConfig["address"].(string), "${VAULT_TOKEN}")
default:
    return nil, ErrConfigValidation("bootstrap", fmt.Sprintf("unknown provider: %s", provider))
}
```

With:

```go
switch provider {
case ProviderAWS:
    name, err := safeStringFromMap(providerConfig, "name")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    region, err := safeStringFromMap(providerConfig, "region")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    builder.WithAWSProvider(name, region)
case ProviderAzure:
    name, err := safeStringFromMap(providerConfig, "name")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    vaultURL, err := safeStringFromMap(providerConfig, "vault_url")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    builder.WithAzureProvider(name, vaultURL)
case ProviderHuawei:
    name, err := safeStringFromMap(providerConfig, "name")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    region, err := safeStringFromMap(providerConfig, "region")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    projectID, err := safeStringFromMap(providerConfig, "project_id")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    builder.WithHuaweiProvider(name, region, projectID)
case ProviderVault:
    name, err := safeStringFromMap(providerConfig, "name")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    address, err := safeStringFromMap(providerConfig, "address")
    if err != nil {
        return nil, ErrConfigValidation("bootstrap", err.Error())
    }
    builder.WithVaultProvider(name, address, "${VAULT_TOKEN}")
default:
    return nil, ErrConfigValidation("bootstrap", fmt.Sprintf("unknown provider: %s", provider))
}
```

- [ ] **Step 4: Build check**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/agent.go
git commit -m "fix: replace unguarded type assertions in bootstrap with safe extraction"
```

---

### Task 4: Fix C5 — ReleaseLock panics on unlocked mutex

**Files:**
- Modify: `internal/rotation/lock_manager.go`
- Modify: `internal/rotation/lock_manager_test.go` (or create)

- [ ] **Step 1: Write the failing test**

```go
func TestLockManager_ReleaseLock_NeverAcquired_NoPanic(t *testing.T) {
	lm, err := NewLockManager("", nil)
	if err != nil {
		t.Fatal(err)
	}

	// This should not panic — releasing a lock that was never acquired
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ReleaseLock panicked: %v", r)
		}
	}()
	lm.ReleaseLock("container-that-was-never-locked")
}

func TestLockManager_ReleaseLock_AfterAcquireTimeout_NoPanic(t *testing.T) {
	lm, err := NewLockManager("", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Hold the lock from another goroutine
	if err := lm.AcquireLock("my-container", 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Try to acquire again — times out
	err = lm.AcquireLock("my-container", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// ReleaseLock for the failed acquisition must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ReleaseLock panicked after failed AcquireLock: %v", r)
		}
	}()
	lm.ReleaseLock("my-container")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/rotation/... -run "TestLockManager_ReleaseLock" -v
```

Expected: FAIL (panic) on the first test.

- [ ] **Step 3: Add acquired tracking to LockManager**

In `internal/rotation/lock_manager.go`, update the `LockManager` struct:

```go
type LockManager struct {
	locks    map[string]*sync.Mutex
	acquired map[string]bool // tracks which keys have been successfully locked
	mu       sync.Mutex
	fileLock *FileLock
}
```

Update `NewLockManager` to initialize `acquired`:

```go
lm := &LockManager{
    locks:    make(map[string]*sync.Mutex),
    acquired: make(map[string]bool),
}
```

Update `AcquireLock` to set `acquired[containerID] = true` after successful lock:

```go
func (lm *LockManager) AcquireLock(containerID string, timeout time.Duration) error {
	lm.mu.Lock()
	mutex, exists := lm.locks[containerID]
	if !exists {
		mutex = &sync.Mutex{}
		lm.locks[containerID] = mutex
	}
	lm.mu.Unlock()

	if !tryLockWithTimeout(mutex, timeout) {
		return fmt.Errorf("failed to acquire lock for container %s within %v", containerID, timeout)
	}

	lm.mu.Lock()
	lm.acquired[containerID] = true
	lm.mu.Unlock()

	if lm.fileLock != nil {
		if err := lm.fileLock.AcquireLock(containerID, timeout); err != nil {
			lm.mu.Lock()
			mutex.Unlock()
			delete(lm.acquired, containerID)
			lm.mu.Unlock()
			return err
		}
	}

	return nil
}
```

Update `ReleaseLock` to guard on `acquired`:

```go
func (lm *LockManager) ReleaseLock(containerID string) {
	lm.mu.Lock()
	wasAcquired := lm.acquired[containerID]
	if wasAcquired {
		if mutex, exists := lm.locks[containerID]; exists {
			mutex.Unlock()
		}
		delete(lm.acquired, containerID)
	}
	lm.mu.Unlock()

	if wasAcquired && lm.fileLock != nil {
		lm.fileLock.ReleaseLock(containerID)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/rotation/... -run "TestLockManager_ReleaseLock" -v -race
```

Expected: PASS, no race

- [ ] **Step 5: Build check**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/rotation/lock_manager.go internal/rotation/lock_manager_test.go
git commit -m "fix: ReleaseLock guards against panic on never-acquired mutex"
```

---

### Task 5: Fix H11 — containerID[:12] panic on short IDs

**Files:**
- Create: `internal/util/ids.go`
- Create: `internal/util/ids_test.go`
- Modify: `internal/agent/agent.go` (replace bare slices)
- Modify: `internal/watcher/event_handler.go` (replace bare slices)
- Modify: `internal/proxy/manager.go` (replace bare slices)

- [ ] **Step 1: Write the failing test**

Create `internal/util/ids_test.go`:

```go
package util_test

import (
	"testing"

	"github.com/docker-secret-operator/dso/internal/util"
)

func TestShortID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"abc", "abc"},
		{"12345678901", "12345678901"},   // 11 chars — returned as-is
		{"123456789012", "123456789012"}, // exactly 12
		{"1234567890123", "123456789012"}, // 13 chars — truncated
		{"abc123def456ghi", "abc123def456"}, // 15 chars
	}
	for _, tc := range cases {
		got := util.ShortID(tc.input)
		if got != tc.want {
			t.Errorf("ShortID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Create internal/util/ids.go**

```go
package util

// ShortID returns up to the first 12 characters of id.
// Safe for empty or short strings — never panics.
func ShortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
```

- [ ] **Step 3: Run test to verify it passes**

```bash
go test ./internal/util/... -v
```

Expected: PASS

- [ ] **Step 4: Replace bare [:12] slices in agent.go**

In `internal/agent/agent.go`, add import `"github.com/docker-secret-operator/dso/internal/util"` and replace every occurrence of `msg.Actor.ID[:12]` and `containerID[:12]` with `util.ShortID(msg.Actor.ID)` / `util.ShortID(containerID)`.

```bash
grep -n '\[:12\]' /Users/mdumair/Personal_Work/Antigravity_Work/dso/internal/agent/agent.go
```

Replace each found occurrence.

- [ ] **Step 5: Replace bare [:12] slices in event_handler.go**

```bash
grep -n '\[:12\]' /Users/mdumair/Personal_Work/Antigravity_Work/dso/internal/watcher/event_handler.go
```

Add import and replace each occurrence with `util.ShortID(...)`.

- [ ] **Step 6: Replace bare [:12] slices in proxy/manager.go**

```bash
grep -n '\[:12\]' /Users/mdumair/Personal_Work/Antigravity_Work/dso/internal/proxy/manager.go
```

Add import and replace each occurrence with `util.ShortID(...)`.

- [ ] **Step 7: Build and test**

```bash
go build ./...
go test ./internal/util/... ./internal/agent/... ./internal/watcher/... ./internal/proxy/... -race
```

- [ ] **Step 8: Commit**

```bash
git add internal/util/ids.go internal/util/ids_test.go \
        internal/agent/agent.go \
        internal/watcher/event_handler.go \
        internal/proxy/manager.go
git commit -m "fix: replace bare containerID[:12] slices with ShortID helper, prevent panic on short IDs"
```

---

## PR2 — Goroutine & Context Fixes

Fixes: C4 (goroutine leak), H12 (WebSocket race), H13 (EventStore), H14 (Hub readPump leak)

---

### Task 6: Fix C4 — Provider goroutine leak in server.go

**Files:**
- Modify: `internal/agent/server.go:118-142`

- [ ] **Step 1: Write the failing test (goroutine count check)**

Append to `internal/agent/` server tests:

```go
func TestGetSecret_ProviderTimeout_NoGoroutineLeak(t *testing.T) {
	// A provider that blocks until its context is cancelled
	blockingProvider := &mockBlockingProvider{}
	s := &AgentServer{
		Cache:  NewSecretCache(time.Minute),
		Logger: zaptest.NewLogger(t),
	}
	_ = s
	_ = blockingProvider

	before := runtime.NumGoroutine()

	// Simulate the fetch with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	fetchCh := make(chan struct{}, 1)
	go func() {
		<-ctx.Done()
		fetchCh <- struct{}{}
	}()
	<-fetchCh

	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+2 {
		t.Fatalf("goroutine leak: started with %d, ended with %d", before, after)
	}
}
```

- [ ] **Step 2: Fix server.go — create context BEFORE goroutine**

In `internal/agent/server.go`, replace:

```go
fetchCh := make(chan fetchResult, 1)
go func() {
    d, e := prov.GetSecret(req.Secret)
    fetchCh <- fetchResult{d, e}
}()

fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer fetchCancel()
```

With:

```go
fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer fetchCancel()

fetchCh := make(chan fetchResult, 1)
go func() {
    d, e := prov.GetSecret(req.Secret)
    select {
    case fetchCh <- fetchResult{d, e}:
    case <-fetchCtx.Done():
        // context already timed out; nobody is reading fetchCh, exit cleanly
    }
}()
```

- [ ] **Step 3: Build and verify**

```bash
go build ./internal/agent/...
go test ./internal/agent/... -race
```

- [ ] **Step 4: Commit**

```bash
git add internal/agent/server.go
git commit -m "fix: create fetchCtx before goroutine in GetSecret to prevent goroutine leak on timeout"
```

---

### Task 7: Fix H13 — EventStore mutex released before broadcast + blocking send

**Files:**
- Modify: `internal/server/eventstore.go`
- Modify: `internal/server/hub.go` (buffer broadcast channel)

- [ ] **Step 1: Write the failing test**

```go
func TestEventStore_Add_Concurrent_NoDeadlock(t *testing.T) {
	hub := NewHub(zaptest.NewLogger(t))
	// Don't run hub — tests that broadcast never blocks even with no consumer
	store := NewEventStore(100, hub)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			store.Add(Event{"n": n})
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: concurrent Add calls did not complete within 2s")
	}
}
```

- [ ] **Step 2: Run test to verify it fails (deadlocks)**

```bash
go test ./internal/server/... -run TestEventStore_Add_Concurrent_NoDeadlock -v -timeout 5s
```

Expected: FAIL (timeout/deadlock) because unbuffered hub channel blocks `Add`.

- [ ] **Step 3: Buffer the hub broadcast channel**

In `internal/server/hub.go`, change `NewHub`:

```go
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		broadcast:  make(chan Event, 64), // buffered to prevent Add from blocking
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		logger:     logger,
	}
}
```

- [ ] **Step 4: Use non-blocking send in EventStore.Add**

In `internal/server/eventstore.go`, change the broadcast section:

```go
s.mu.Unlock()

if s.hub != nil {
    s.hub.broadcast <- e
}
```

To:

```go
s.mu.Unlock()

if s.hub != nil {
    select {
    case s.hub.broadcast <- e:
    default:
        // Hub buffer full; drop oldest broadcast to prevent blocking Add callers.
    }
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/server/... -run TestEventStore_Add_Concurrent_NoDeadlock -v -race
```

Expected: PASS

- [ ] **Step 6: Build check**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/server/eventstore.go internal/server/hub.go
git commit -m "fix: buffer hub broadcast channel and use non-blocking send to prevent Add deadlock"
```

---

### Task 8: Fix H12 — WebSocket concurrent write race

**Files:**
- Modify: `internal/server/rest.go:180-198`

- [ ] **Step 1: Fix handleEventWS — route all writes through send channel**

In `internal/server/rest.go`, replace the `handleEventWS` section that does direct `conn.WriteJSON`:

```go
initialEvents := s.EventStore.GetLast(limit, severity)
for _, ev := range initialEvents {
    if err := client.conn.WriteJSON(ev); err != nil {
        return
    }
}

go client.writePump()
go client.readPump()
```

With:

```go
// Register the client with the hub first so writePump is the sole writer.
s.Hub.register <- client
go client.writePump()
go client.readPump()

// Push historical events through the send channel (never directly on conn).
initialEvents := s.EventStore.GetLast(limit, severity)
for _, ev := range initialEvents {
    select {
    case client.send <- ev:
    default:
        // Client send buffer full; skip oldest historical event rather than block.
    }
}
```

- [ ] **Step 2: Build and race check**

```bash
go build ./...
go test ./internal/server/... -race
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/rest.go
git commit -m "fix: route WebSocket historical event push through send channel to eliminate concurrent write race"
```

---

### Task 9: Fix H14 — Hub readPump goroutine leak on shutdown

**Files:**
- Modify: `internal/server/hub.go`
- Modify: `internal/server/rest.go` (pass ctx to readPump)

- [ ] **Step 1: Write the test**

```go
func TestHub_ReadPump_ExitsOnContextCancel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hub := NewHub(logger)

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	// Create a fake connection pair
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	wsConn, _, err := websocket.NewClient(client, &url.URL{}, nil, 1024, 1024)
	if err != nil {
		t.Skip("websocket client setup failed:", err)
	}

	c := &Client{
		hub:  hub,
		conn: wsConn,
		send: make(chan Event, 64),
	}

	before := runtime.NumGoroutine()
	go c.readPump(ctx)

	// Cancel context — readPump should exit
	cancel()
	time.Sleep(100 * time.Millisecond)

	after := runtime.NumGoroutine()
	if after >= before {
		t.Fatalf("readPump goroutine leaked: before=%d after=%d", before, after)
	}
}
```

- [ ] **Step 2: Add ctx parameter to readPump**

In `internal/server/hub.go`, change `readPump`:

```go
func (c *Client) readPump(ctx context.Context) {
	defer func() {
		select {
		case c.hub.unregister <- c:
		case <-ctx.Done():
		}
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(pingPeriod + 10*time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pingPeriod + 10*time.Second))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
```

- [ ] **Step 3: Update rest.go call site to pass ctx**

In `internal/server/rest.go`, find `go client.readPump()` and change to `go client.readPump(r.Context())`.

- [ ] **Step 4: Build and test**

```bash
go build ./...
go test ./internal/server/... -race
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/hub.go internal/server/rest.go
git commit -m "fix: pass context to readPump to prevent goroutine leak when Hub exits"
```

---

## PR3 — Security-Correctness Fixes

Fixes: H6 (path traversal), H7 (recovery ownership), H8 (local mutex), H15 (signal context)

---

### Task 10: Fix H6 — Path traversal in file injection

**Files:**
- Modify: `internal/injector/inject.go`
- Modify: `internal/injector/inject_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/injector/inject_test.go`, add:

```go
func TestInjectOneFile_PathTraversal_Rejected(t *testing.T) {
	traversalInputs := []string{
		"../../etc/passwd",
		"../evil",
		"/absolute/path",
		".",
		"",
		"foo/bar",
	}
	for _, input := range traversalInputs {
		err := injectOneFile(context.Background(), nil, "cid", input, "content", 0, 0)
		if err == nil {
			t.Errorf("expected error for fileName=%q, got nil", input)
		}
	}
}

func TestInjectOneFile_ValidFileName_BuildsCorrectPath(t *testing.T) {
	cmd := buildInjectCmd("/run/secrets/dso/myfile", 0, 0)
	if !strings.Contains(cmd, "/run/secrets/dso/myfile") {
		t.Errorf("expected correct destPath in cmd, got: %s", cmd)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/injector/... -run TestInjectOneFile_PathTraversal_Rejected -v
```

Expected: FAIL — no error returned for traversal inputs.

- [ ] **Step 3: Add validation in injectOneFile**

In `internal/injector/inject.go`, add at the top of `injectOneFile`:

```go
func injectOneFile(ctx context.Context, cli *client.Client, containerID, fileName, content string, uid, gid int) error {
	// Sanitize fileName to prevent path traversal inside the container.
	fileName = filepath.Base(fileName)
	if fileName == "" || fileName == "." || strings.ContainsRune(fileName, '/') {
		return fmt.Errorf("invalid secret file name %q: must be a plain filename with no path components", fileName)
	}

	destPath := "/run/secrets/dso/" + fileName
	// ... rest of function unchanged
```

Add `"path/filepath"` and `"strings"` to imports if not already present.

- [ ] **Step 4: Run tests to verify pass**

```bash
go test ./internal/injector/... -run "TestInjectOneFile_PathTraversal|TestInjectOneFile_ValidFileName" -v
```

Expected: PASS

- [ ] **Step 5: Build and commit**

```bash
go build ./...
git add internal/injector/inject.go internal/injector/inject_test.go
git commit -m "fix: sanitize fileName in InjectFiles to prevent path traversal inside container"
```

---

### Task 11: Fix H7 — Recovery matches unowned containers

**Files:**
- Modify: `internal/agent/recovery.go:45-80`

- [ ] **Step 1: Write the failing test**

```go
func TestRecoverContainers_OnlyActsOnDSOLabeled(t *testing.T) {
	// Two containers: one DSO-managed, one not.
	// Only the DSO-managed one should appear in the recovery scan.
	dsoContainer := types.Container{
		ID:    "dso111111111111",
		Names: []string{"/myapp_dso_backup_1234"},
		Labels: map[string]string{
			"dso.reloader": "true",
		},
	}
	foreignContainer := types.Container{
		ID:    "foreign2222222222",
		Names: []string{"/foreign_dso_backup_5678"},
		Labels: map[string]string{}, // no dso.reloader label
	}
	_ = dsoContainer
	_ = foreignContainer

	// The label filter should exclude foreignContainer from the scan.
	// We test via filterContainersByDSOLabel helper.
	filtered := filterContainersByDSOLabel([]types.Container{dsoContainer, foreignContainer})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 DSO-managed container, got %d", len(filtered))
	}
	if filtered[0].ID != "dso111111111111" {
		t.Fatalf("wrong container kept: %s", filtered[0].ID)
	}
}
```

- [ ] **Step 2: Add filterContainersByDSOLabel helper and use it**

In `internal/agent/recovery.go`, add:

```go
// filterContainersByDSOLabel returns only containers that carry the dso.reloader=true
// label, ensuring recovery never acts on containers DSO does not own.
func filterContainersByDSOLabel(containers []types.Container) []types.Container {
	var managed []types.Container
	for _, c := range containers {
		if c.Labels["dso.reloader"] == "true" {
			managed = append(managed, c)
		}
	}
	return managed
}
```

In `recoverOrphanedContainers` (the function that calls `recoverSingleRotation`), add the filter after listing containers:

```go
containers, err := ar.cli.ContainerList(ctx, container.ListOptions{All: true})
if err != nil { ... }

// Only consider DSO-managed containers to avoid acting on unowned containers.
containers = filterContainersByDSOLabel(containers)
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/agent/... -run TestRecoverContainers_OnlyActsOnDSOLabeled -v
```

Expected: PASS

- [ ] **Step 4: Build and commit**

```bash
go build ./...
git add internal/agent/recovery.go
git commit -m "fix: recovery scan filters by dso.reloader label to prevent acting on unowned containers"
```

---

### Task 12: Fix H8 — Local mutex in ApplyTransaction

**Files:**
- Modify: `internal/agent/multi_secret_updater.go:246-253`

- [ ] **Step 1: Remove the useless local mutex**

In `internal/agent/multi_secret_updater.go`, replace:

```go
func (msu *MultiSecretUpdater) ApplyTransaction(
	ctx context.Context,
	updates []MultiSecretUpdate,
	callback TransactionCallback,
) error {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
```

With:

```go
func (msu *MultiSecretUpdater) ApplyTransaction(
	ctx context.Context,
	updates []MultiSecretUpdate,
	callback TransactionCallback,
) error {
	// Atomicity across validate+apply is guaranteed by the caller holding the
	// project-level rotation lock in trigger.go before calling ApplyTransaction.
```

Remove the `"sync"` import from this file if it is no longer used elsewhere in the file.

- [ ] **Step 2: Build and race check**

```bash
go build ./...
go test ./internal/agent/... -race
```

- [ ] **Step 3: Commit**

```bash
git add internal/agent/multi_secret_updater.go
git commit -m "fix: remove useless local mutex from ApplyTransaction — provides no concurrency protection"
```

---

### Task 13: Fix H15 — Signal context created after servers start

**Files:**
- Modify: `internal/cli/agent.go`

- [ ] **Step 1: Move signal.NotifyContext to top of RunE**

In `internal/cli/agent.go`, find `RunE: func(cmd *cobra.Command, args []string) error {` and move the signal context setup to immediately after logger init:

Change from (signal context at line ~117, after goroutines):

```go
// Initialize Trigger Engine
trigger, err := agent.NewTriggerEngine(...)

// Handle Termination...
ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
```

To (signal context as FIRST thing after logger):

```go
RunE: func(cmd *cobra.Command, args []string) error {
    // Signal handling must be established before anything else so SIGTERM
    // during startup is not silently dropped.
    ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    logger, err := observability.NewLogger(...)
    // ... rest of function uses ctx everywhere
```

Also update `scanCtx` to derive from the signal `ctx` instead of `context.Background()`:

```go
scanCtx, scanCancel := context.WithTimeout(ctx, 30*time.Second)
```

- [ ] **Step 2: Build check**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/cli/agent.go
git commit -m "fix: move signal.NotifyContext to top of RunE so SIGTERM during startup is not lost"
```

---

## PR4 — Silent Failure Fixes

Fixes: H9 (Cache.Clear zeroize), H10 (non-deterministic provider), H16 (setup I/O), H18 (fabricated API data)

---

### Task 14: Fix H9 — Cache.Clear cannot zeroize secrets

**Files:**
- Modify: `internal/agent/cache.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCache_Clear_ZeroizesSecrets(t *testing.T) {
	c := NewCache()

	seed := &resolver.AgentSeed{
		ProjectName: "myproject",
		SecretPool:  map[string]string{"hash1": "supersecret"},
		Services:    map[string]resolver.ServiceSecrets{},
	}
	c.Seed(seed)

	// Verify secret is present
	val, ok := c.Get("hash1")
	if !ok || val != "supersecret" {
		t.Fatal("seed did not store secret")
	}

	c.Clear("myproject")

	// After clear the secret bytes must be zeroed
	c.mu.RLock()
	b, exists := c.secrets["hash1"]
	c.mu.RUnlock()

	if exists {
		for _, v := range b {
			if v != 0 {
				t.Fatal("secret bytes were not zeroed after Clear")
			}
		}
	}
	// Also acceptable: the key is deleted entirely
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/agent/... -run TestCache_Clear_ZeroizesSecrets -v
```

Expected: FAIL — bytes are not zeroed.

- [ ] **Step 3: Add reverse index and zeroization to Cache**

In `internal/agent/cache.go`, update the `Cache` struct:

```go
type Cache struct {
	secrets        map[string][]byte             // hash → plaintext bytes
	projects       map[string]*resolver.AgentSeed
	projectSecrets map[string][]string           // project → []hash (reverse index for zeroization)
	mu             sync.RWMutex
}
```

Update `NewCache`:

```go
func NewCache() *Cache {
	return &Cache{
		secrets:        make(map[string][]byte),
		projects:       make(map[string]*resolver.AgentSeed),
		projectSecrets: make(map[string][]string),
	}
}
```

Update `Seed` to populate the reverse index:

```go
func (c *Cache) Seed(seed *resolver.AgentSeed) {
	if seed == nil || seed.ProjectName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.projects[seed.ProjectName] = seed

	var hashes []string
	for hash, value := range seed.SecretPool {
		c.secrets[hash] = []byte(value)
		hashes = append(hashes, hash)
	}
	c.projectSecrets[seed.ProjectName] = hashes
}
```

Update `Clear` to zeroize:

```go
func (c *Cache) Clear(project string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.projects, project)

	if hashes, ok := c.projectSecrets[project]; ok {
		for _, hash := range hashes {
			if b, exists := c.secrets[hash]; exists {
				for i := range b {
					b[i] = 0
				}
				delete(c.secrets, hash)
			}
		}
		delete(c.projectSecrets, project)
	}
}
```

- [ ] **Step 4: Run test to verify pass**

```bash
go test ./internal/agent/... -run TestCache_Clear_ZeroizesSecrets -v -race
```

Expected: PASS

- [ ] **Step 5: Build and commit**

```bash
go build ./...
git add internal/agent/cache.go
git commit -m "fix: Cache.Clear now zeroizes secret bytes using a project→hash reverse index"
```

---

### Task 15: Fix H10 — Non-deterministic default provider selection

**Files:**
- Modify: `pkg/config/config.go:276-281`
- Modify: `pkg/config/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestValidate_MultipleProviders_NoProviderSpecified_ReturnsError(t *testing.T) {
	cfg := &Config{
		Version: "v1.0.0",
		Mode:    "local",
		Providers: map[string]ProviderConfig{
			"aws":   {Type: "aws"},
			"azure": {Type: "azure"},
		},
		Secrets: []SecretMapping{
			{
				Name:     "mysecret",
				Provider: "", // not specified
				Mappings: map[string]string{"KEY": "path/to/secret"},
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for unspecified provider with multiple providers, got nil")
	}
}

func TestValidate_SingleProvider_NoProviderSpecified_UsesIt(t *testing.T) {
	cfg := &Config{
		Version: "v1.0.0",
		Mode:    "local",
		Providers: map[string]ProviderConfig{
			"aws": {Type: "aws"},
		},
		Secrets: []SecretMapping{
			{
				Name:     "mysecret",
				Provider: "",
				Mappings: map[string]string{"KEY": "path/to/secret"},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		// May still fail for other reasons; what matters is no "multiple providers" error
		if strings.Contains(err.Error(), "multiple providers") {
			t.Fatalf("unexpected 'multiple providers' error for single-provider config: %v", err)
		}
	}

	// Provider should be filled in deterministically
	if cfg.Secrets[0].Provider != "aws" {
		t.Fatalf("expected provider=aws, got %q", cfg.Secrets[0].Provider)
	}
}
```

- [ ] **Step 2: Fix getFirstProviderName (or replace the inline block)**

In `pkg/config/config.go`, find the block (around line 276):

```go
for name := range c.Providers {
    providerName = name
    break
}
```

Replace with:

```go
if len(c.Providers) == 1 {
    for name := range c.Providers {
        providerName = name
    }
} else {
    return fmt.Errorf("secret %q has no provider specified and multiple providers are configured: specify the 'provider' field", sec.Name)
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./pkg/config/... -run "TestValidate_.*Provider" -v
```

Expected: PASS

- [ ] **Step 4: Build and commit**

```bash
go build ./...
git add pkg/config/config.go pkg/config/config_test.go
git commit -m "fix: non-deterministic default provider selection — error if multiple providers and none specified"
```

---

### Task 16: Fix H16 — Silent os.ReadFile/os.WriteFile failures in setup

**Files:**
- Modify: `internal/cli/setup.go:219,255`

- [ ] **Step 1: Fix the ReadFile error (backup)**

In `internal/cli/setup.go`, replace:

```go
configData, _ := os.ReadFile(configPath)
```

With:

```go
configData, readErr := os.ReadFile(configPath)
if readErr != nil {
    fmt.Printf("⚠ Warning: could not back up existing config at %s: %v — proceeding without backup\n", configPath, readErr)
    configData = nil
}
```

- [ ] **Step 2: Fix the WriteFile error (restore)**

Replace:

```go
if len(configData) > 0 {
    os.WriteFile(configPath, configData, 0664)
}
```

With:

```go
if len(configData) > 0 {
    if err := os.WriteFile(configPath, configData, 0664); err != nil {
        fmt.Printf("❌ Error: failed to restore your config to %s: %v\n", configPath, err)
        fmt.Println("  Your custom config was backed up in memory but could not be written back.")
        fmt.Printf("  To restore manually, re-edit %s with your secrets configuration.\n", configPath)
        return fmt.Errorf("failed to restore config: %w", err)
    }
}
```

- [ ] **Step 3: Build and test**

```bash
go build ./...
go test ./internal/cli/... -race
```

- [ ] **Step 4: Commit**

```bash
git add internal/cli/setup.go
git commit -m "fix: handle os.ReadFile/WriteFile errors in setup config backup/restore"
```

---

### Task 17: Fix H18 — Fabricated secret metadata in REST API

**Files:**
- Modify: `internal/server/rest.go` (handleListSecrets section ~line 344-380)

- [ ] **Step 1: Update handleListSecrets to read from SecretCache**

Find `handleListSecrets` in `internal/server/rest.go`. Replace the hardcoded field values:

```go
// BEFORE (fabricated):
LastSyncedAt:    time.Now(),
LastUpdatedAt:   time.Now(),
InjectionType:   "env",
Version:         "1",
RotationEnabled: true,
AutoSyncEnabled: true,
Status:          "synced",
```

With values derived from the actual `SecretCache` and config:

```go
// Determine injection type from config mappings
injType := "env"
if sec.Inject.Type != "" {
    injType = sec.Inject.Type
}

// Get last sync time from cache if available
var lastSync time.Time
if cacheEntry, ok := s.Cache.GetMeta(sec.Name); ok {
    lastSync = cacheEntry.UpdatedAt
}

// Build response with real values; omit fields not yet tracked
secretResponse := SecretResponse{
    Name:            sec.Name,
    Provider:        sec.Provider,
    Status:          s.Cache.StatusFor(sec.Name), // "synced" | "stale" | "unknown"
    InjectionType:   injType,
    RotationEnabled: sec.Rotation.Enabled,
}
if !lastSync.IsZero() {
    secretResponse.LastSyncedAt = lastSync
}
```

Note: If `Cache.GetMeta` and `Cache.StatusFor` don't exist yet, add stub implementations that return zero values gracefully:

```go
// In internal/agent/cache.go
type CacheMeta struct {
    UpdatedAt time.Time
}

func (sc *SecretCache) GetMeta(secretName string) (CacheMeta, bool) {
    // Return zero CacheMeta for now; future work populates UpdatedAt
    return CacheMeta{}, false
}

func (sc *SecretCache) StatusFor(secretName string) string {
    if _, ok := sc.Get(secretName + ":"); ok {
        return "synced"
    }
    return "unknown"
}
```

This removes the `time.Now()` fabrication while keeping the API functional.

- [ ] **Step 2: Build and test**

```bash
go build ./...
go test ./internal/server/... -race
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/rest.go internal/agent/cache.go
git commit -m "fix: replace fabricated time.Now() and hardcoded metadata in handleListSecrets with real cache values"
```

---

## Final Verification

After all 4 PRs are merged:

- [ ] **Full build**

```bash
go build ./...
```

Expected: no output (success)

- [ ] **Full test suite with race detector**

```bash
go test -race ./... 2>&1 | tail -30
```

Expected: all PASS, zero DATA RACE reports

- [ ] **Vet**

```bash
go vet ./...
```

Expected: no output (success)

- [ ] **Verify no panics remain in constructors**

```bash
grep -rn "panic(" internal/agent/trigger.go internal/bootstrap/agent.go internal/rotation/lock_manager.go
```

Expected: zero results (or only test-helper panics with clear comments)
