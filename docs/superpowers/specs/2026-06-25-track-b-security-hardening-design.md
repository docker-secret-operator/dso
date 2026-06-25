# Track B — Security Hardening

**Date:** 2026-06-25  
**Status:** Approved for implementation  
**Scope:** 5 security bugs (2 HIGH, 3 MEDIUM) identified by fresh post-Track-A audit  
**Approach:** 2 focused PRs — one for server/network hardening, one for input validation

---

## Background

Track A closed all crash/correctness bugs. This audit scanned the remaining surface for exploitable security issues. All 5 issues are in the networked components (REST API, Unix socket) or the input-validation layer (provider plugin paths, auth token strength). No secrets were found hardcoded; crypto (AES-256-GCM with random nonces) and timing-safe comparisons are already in place.

---

## PR5 — Server Hardening

**Files:** `internal/agent/server.go`, `internal/server/rest.go`

### B1 — Unix socket TOCTOU window (HIGH)

**Problem:** `net.Listen("unix", socketPath)` creates the socket file with permissions derived from the process umask (typically 0755 or 0644 on most systems). `os.Chmod(socketPath, 0600)` is called only *after* the socket exists and is listening. During the window between Listen and Chmod, any local user can connect to the agent socket and issue privileged IPC commands (inject secrets, trigger rotation).

**Fix:** Set the process umask to `0077` (block all group+other bits) immediately before calling `net.Listen`, then restore the original umask after `Chmod` succeeds. This ensures the socket file is created with at most `0700` from the kernel's side, and the explicit `Chmod` then tightens it to `0600` or `0660`. Use `unix.Umask` on Linux; wrap in `//go:build` constraint to keep the non-Linux build.

```go
// Save and tighten umask before socket creation to close TOCTOU window.
old := unix.Umask(0077)
listener, err := net.Listen("unix", socketPath)
unix.Umask(old)
```

Add a `//go:build linux` file `internal/agent/umask_linux.go` and a no-op `umask_other.go` so the build stays cross-platform.

**Test:** Start a socket server on a temp path; before `Chmod` fires, stat the socket file and assert its mode bits don't allow group/other access.

### B2 — Unbounded request body on REST endpoints (HIGH)

**Problem:** `handleSecretUpdate` decodes the request body with `json.NewDecoder(r.Body).Decode(...)` and no call to `http.MaxBytesReader`. An unauthenticated (or authenticated) client can stream a multi-gigabyte JSON body and exhaust the agent's heap. The agent runs as root and holds all secret state — an OOM kill takes down the entire secret delivery pipeline.

Also affects `/api/events` and `/api/logs` query params — currently no per-request body size guard.

**Fix:** Add a `maxBodyBytes` middleware (or inline `MaxBytesReader`) at the top of the `ServeHTTP` handler, applied to all non-WebSocket requests:

```go
const maxRequestBodyBytes = 64 * 1024 // 64 KB

func (s *RESTServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path != "/api/events/ws" {
        r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
    }
    // ... existing routing
}
```

Requests that exceed 64 KB receive `413 Request Entity Too Large` before any parsing.

**Test:** POST a 1 MB body to `/api/events/secret-update`; assert the handler returns 413 and the server process RSS does not grow significantly.

---

## PR6 — Input Validation & Auth Hardening

**Files:** `internal/server/rest.go`, `internal/auth/auth.go`, `internal/bootstrap/provider_plugins.go`

### B3 — Missing security response headers (MEDIUM)

**Problem:** The REST API returns no security headers. Browsers or proxies caching secret API responses could expose them to subsequent requests. Missing `X-Content-Type-Options: nosniff` allows MIME sniffing attacks if any endpoint ever returns untrusted content.

**Fix:** Add a `secureHeaders` middleware function applied to all responses:

```go
func secureHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("X-Frame-Options", "DENY")
        w.Header().Set("Cache-Control", "no-store")
        w.Header().Set("Referrer-Policy", "no-referrer")
        next.ServeHTTP(w, r)
    })
}
```

Wrap the mux with this in `StartRESTServer`.

**Test:** Issue a GET to `/health`; assert all four headers are present in the response.

### B4 — DSO_AUTH_TOKEN accepts trivially short tokens (MEDIUM)

**Problem:** `NewAuthenticator()` accepts any non-empty string as a valid token, including single-character values. A 1-byte token is brute-forceable in microseconds. There is also no maximum length, so a 1 MB token would cause every request to allocate and compare 1 MB.

**Fix:** In `StartRESTServer` (where the token is read from the environment), validate token strength before the server starts:

```go
const minTokenBytes = 16
const maxTokenBytes = 512

token := os.Getenv("DSO_AUTH_TOKEN")
if token != "" {
    if len(token) < minTokenBytes {
        return nil, fmt.Errorf(
            "DSO_AUTH_TOKEN is too short (%d bytes); minimum is %d bytes for security",
            len(token), minTokenBytes)
    }
    if len(token) > maxTokenBytes {
        return nil, fmt.Errorf(
            "DSO_AUTH_TOKEN exceeds maximum length (%d bytes); maximum is %d bytes",
            len(token), maxTokenBytes)
    }
}
```

**Test:** Start the server with a 5-character token; assert startup returns an error. Start with a 16-character token; assert startup succeeds.

### B5 — Provider name not validated before use in file paths (MEDIUM)

**Problem:** `buildAndInstallPlugin` constructs `pluginBinary = filepath.Join(pluginDir, "dso-provider-"+provider)` and `cmdDir = filepath.Join("cmd/plugins", "dso-provider-"+provider)` where `provider` is received from the caller. If a malicious provider name contains `..` or `/` components (e.g. `"../../../bin/evil"`), `filepath.Join` will resolve these, allowing the attacker to write a binary to an arbitrary path on the filesystem.

The call chain is: `InstallProviderPlugins(ctx, providers []string)` — the `providers` slice originates from `bootstrap/agent.go` which derives it from user-supplied config. A compromised or malicious `dso.yaml` could supply an arbitrary provider name.

**Fix:** Validate the provider name against a hardcoded allowlist of known providers before using it in any path construction:

```go
var validProviders = map[string]bool{
    "aws": true, "azure": true, "vault": true, "huawei": true,
}

func validateProviderName(provider string) error {
    if !validProviders[provider] {
        return fmt.Errorf("unknown provider %q: must be one of aws, azure, vault, huawei", provider)
    }
    return nil
}
```

Call `validateProviderName(provider)` at the top of `buildAndInstallPlugin`.

**Test:** Call `buildAndInstallPlugin` with `provider = "../../../bin/evil"`; assert it returns an error without touching the filesystem.

---

## Implementation Order

1. **PR5** first — server hardening closes network-level DoS and privilege escalation vectors
2. **PR6** next — input validation and auth hardening

---

## Success Criteria

- `go build ./...` passes
- `go test -race ./...` passes with no new failures
- `go vet ./...` clean
- No `http.MaxBytesReader` missing on any non-WebSocket endpoint
- Security headers present on all responses
- Token length validated at startup, not at request time
- Provider name validated against allowlist before path construction
