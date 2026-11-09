# M0 — PE-11 Security Analysis: Local IPC Socket Authorization

**Date:** 2026-11-08 · **Status:** analysis only, nothing changed · Labels as defined in `m0-baseline-and-reconciliation.md`. Out of scope and untouched: `dso central`, WebUI scope, network RBAC.

## 1. Verdict

PE-11 is **confirmed and worse than recorded.** Authentication of the socket peer is sound; **authorization is absent** — every authenticated peer may call every method with arbitrary arguments, and two methods let the caller choose what the root agent loads and reads. New consequences found this pass:

- **PE-28 (Critical):** a `dso`-group member can read **any root-readable file** and **any agent environment variable** through `GetSecret` (in-process `file`/`env` backends with caller-chosen config).
- **PE-29 (High):** a `dso`-group member can **crash the agent** with two calls (nil `*plugin.Client` dereference inside an RPC handler; `net/rpc` does not recover). The same bug crashes the agent on its own second poll of any configured `file`/`env` provider.
- The previously described provider-config poisoning is **reachable in steady state**, not just at startup (§4.3).

## 2. Authentication layer (good)

| Control | Evidence | Assessment |
|---|---|---|
| Socket file `0660 root:dso` (falls back to `0600` if no `dso` group); dir chowned `root:dso`, created `0750`; umask tightened before `Listen` | `internal/agent/server.go:363,386-423` | Correct; fail-closed on chmod error |
| `SO_PEERCRED` read per connection; reject on error | `server.go:479-502`; `peercred_linux.go` | Correct |
| Policy: uid 0, agent's own uid, primary gid == `dso`, or supplementary member of `dso` (resolved from `/etc/group` by uid) | `internal/agent/peercred.go:59-87` | Correct for its intent. Supplementary membership comes from the account database, not the peer's live credential set (minor: a user removed from `/etc/group` loses access immediately; one added gains it without re-login). Non-NSS users fail closed (documented). |
| Non-Linux: falls back to filesystem permissions only, logs a warning | `server.go:484-490` | Acceptable (production is Linux) |
| Per-connection audit identity | `server.go:323-340,508,521-538` | Good |

## 3. Authorization layer (missing)

Every connection registers the full `AgentServer` method set (`server.go:533`). Exposed RPC methods and what an authorised peer can do:

| Method | Code | Effect for any `dso` member | Risk |
|---|---|---|---|
| `GetEvents` | `server.go:68-77` | Read last 100 agent event strings | Low (metadata) |
| `GetStatus` | `:159-196` | Provider health, degraded services, cache size | Low |
| `TriggerReconciliation` | `:93-152` | Fetch all configured secrets now and rotate changed ones | Low-Med (availability lever; also a PE-31 concurrent caller) |
| `GetSecret` | `:225-321` via `agentConn.GetSecret` `:338-340` | **(a)** fast path returns **any** cached `provider:secret` value (`:226-236`); **(b)** configured provider ⇒ fetch any secret name the provider's credentials can read, not only names in `dso.yaml` (`:257,266,284`); **(c)** unconfigured name ⇒ "legacy one-off" provider with `Type = req.Provider`, `Config = req.Config` (`:257-264`) | **Critical** — see §4 |
| `CheckProviderConnectivity` | `:204-223` | Root agent loads a provider of caller-chosen type with caller-chosen config/region/auth params and **caches it under the caller-chosen name** | **High** — see §4.3 |

## 4. Exploit paths (traced)

### 4.1 Arbitrary root file read — PE-28 (Critical)

`GetSecret{Provider:"file", Config:{"path":"/etc"}, Secret:"shadow"}` → `pCfg = {Type:"file", Config:req.Config}` (`server.go:260-263`) → `GetProvider` → `LoadProvider("file", …)` returns the **in-process** backend (`pkg/provider/load.go:274-283`, no plugin process, no env allow-list, no hash check) → `FileProvider.Init` sets `basePath = config["path"]` → `GetSecret` reads `filepath.Join(basePath, name)`, constrained only by `IsSafePath(basePath, …)` i.e. by the caller's own base (`pkg/backend/file/file.go`). Non-JSON content is returned verbatim as `{"value": …}`. The agent runs as root (`internal/bootstrap/systemd.go:41-64`). The result is also written to the shared cache under `file:shadow` (`server.go:318`).
**Probe:** `TestM0Probe_PE11_FileBackendPathIsCallerControlled` read a file from a caller-chosen directory. **Not executed end-to-end over the socket** (needs a Linux integration test, §8).

### 4.2 Agent environment disclosure — PE-28

`GetSecret{Provider:"env", Secret:"<VAR>"}` → in-process `EnvProvider` → `os.LookupEnv(name)` of the **root agent process** (`pkg/backend/env/env.go`). Anything in the unit's environment (e.g. `DSO_AUTH_TOKEN`, cloud credentials passed by env, `DSO_*` settings) is readable. Code-confirmed; which variables are present depends on deployment (unverified).

### 4.3 Provider-config poisoning (original PE-11) — reachable in steady state

`SecretStoreManager` keys entries by **name only** and returns a cached entry regardless of the config passed (`internal/providers/store.go:41-66,131-137`). A caller-supplied config wins whenever the name is not cached at the moment of the call. The cache is emptied for a name when:

1. the agent has not polled that provider yet (startup; or provider used only by rotation-disabled secrets — never cached by the agent);
2. **`LastHealthy` is older than 10 minutes** (`store.go:51-57`) — the stale check runs **inside the attacker's own call**, which then deletes the entry and re-initialises it with the attacker's config. With the default 5-minute poll and 1.5× adaptive back-off up to 4× (`trigger.go:466-473`: 5 → 7.5 → 11.25 min…), an unchanged secret regularly leaves a >10-minute gap, so this window recurs in normal operation;
3. the plugin process exited (`:58-64`), or 5 consecutive failures evicted it (`:155-170`).

After poisoning, the agent's own polls (`trigger.go:439`) and webhook fetches (`:505`) use the attacker's client: e.g. `type: vault` with an attacker-controlled `address`/`token` ⇒ **attacker-chosen values are injected into production containers** (DB passwords, API keys) and reported as a normal rotation. `CheckProviderConnectivity` uses `Retry.Attempts: 1` (`server.go:209`), so the attacker's call is fast. The concurrent-init race (no single-flight; `store.go:89-137` sleeps between attempts, last `Store` wins) adds another window. **Probe:** `TestM0Probe_PE11_CacheKeyIgnoresConfig` (name occupied by caller-created entry). End-to-end poisoning not executed.

### 4.4 SSRF / credential forwarding

`CheckProviderConnectivity` and the legacy path make the root agent start any **installed** plugin type (path allow-list + manifest still apply, `load.go:289-301`) with a caller-chosen endpoint. The plugin process inherits the agent's allow-listed provider credentials environment (`load.go:151-168`). Concretely:
- Vault: `address` is caller-chosen; HTTPS required for non-loopback (`dso-provider-vault/main.go:28-39`) — outbound requests from the host to any HTTPS endpoint (SSRF, internal-network probing). The token is the caller's own unless omitted.
- Azure/AWS/Huawei: whether a caller-supplied vault URL / endpoint is honoured, and whether the agent's ambient identity (managed identity, instance role) would then authenticate to an attacker endpoint, was **not traced** this pass — **unverified, high-impact if true**; must be checked per plugin before M1 design sign-off.

### 4.5 Denial of service — PE-29 (High)

Native backends are cached with `Client == nil` (`load.go:283,286` → `store.go:131-137`); the next lookup evaluates `entry.Client.Exited()` (`store.go:58`) → nil dereference (`go-plugin v1.7.0 client.go:478-481`). Two `GetSecret{Provider:"env"}` calls panic inside an RPC handler; `net/rpc` has no `recover` ⇒ process exit ⇒ systemd restart ⇒ PE-03 mass re-rotation, and PE-10 proxy outage. **Probe:** `TestM0Probe_PE11_SecondLookupOfNativeBackendPanics`. Separately this crashes a legitimately configured `type: file`/`env` provider on its second poll (inferred end-to-end).

### 4.6 Cache-key collisions

Fast-path cache key is `req.Provider + ":" + req.Secret` (`server.go:226`), the same key space the trigger engine writes (`trigger.go:258,290-301`). §4.1 writes attacker-chosen content under keys like `file:<name>`; if an operator's configured provider is literally named `file` (as in `docs/providers.md:171`), a caller can overwrite the cached value later used by the reloader for that secret (`controller.go:867,955`). Inferred.

## 5. Threat-model framing

`dso` group is documented as the *non-root operator* group (socket `0660 root:dso`, `peercred.go:25-39`). Today it is **root-equivalent for reading** (PE-28) and **able to choose production secret values** (§4.3). If an installation already puts the same people in the `docker` group, they were root-equivalent anyway; the fix matters for installations that use `dso` precisely to avoid granting `docker`.

## 6. Minimal authorization model (proposal for M1 — no network RBAC, no `dso central` change)

Principle: **IPC callers name things; only `dso.yaml` (root:dso 0640) defines them.**

1. **Remove caller-supplied provider configuration from the agent.**
   - `GetSecret`: if `req.Provider` is not a configured provider name ⇒ `permission denied` (delete the legacy branch `server.go:257-264`). Ignore `req.Config` entirely. Restrict `req.Secret` to names listed in `dso.yaml` for that provider.
   - `CheckProviderConnectivity`: root (uid 0 / agent uid) only; build the client in a **throw-away** path (`LoadProvider` directly, `Kill()` after the check), never `Store.GetProvider`, never cached.
   - Native `file`/`env` backends are never constructible from IPC input.
2. **Fix the store** (independent of authz): treat `Client == nil` as "native, always alive" (PE-29); cache key = `name + sha256(canonical(pCfg))`, so a different config can never be returned for a name; single-flight per key.
3. **Tier methods by peer class** (uses the existing `peerIdentity`; table extends ADR-007):

| Method | root / agent uid | `dso` member | other |
|---|---|---|---|
| `GetStatus`, `GetEvents` | allow | allow | deny (already) |
| `TriggerReconciliation` | allow | allow (audited, rate-limited) | deny |
| `GetSecret` value | allow | **deny by default**; allow only if `ipc.secret_readers` lists the user/group for that secret (opt-in, config-file controlled) | deny |
| `CheckProviderConnectivity` | allow (non-caching) | deny | deny |

   Implementation sketch: store `peerIdentity` + resolved class in `agentConn`; override each method on `agentConn` (the pattern already used for `GetSecret`, `server.go:333-340`) to check the class before delegating.
4. **Compatibility:** `docker dso apply` (`internal/cli/apply.go:310`) and `docker dso fetch` (`internal/cli/fetch.go:74`) send the CLI user's provider config. Under the fix they either run as root, or fetch through a configured provider name, or (local mode) bypass the agent. Ship behind `ipc.legacy_caller_config: deny|warn` for one minor release only if the owner wants a transition; default **deny** because PE-28 is critical.
5. **REST leg (F-E1):** in `authOpen` mode (no token, no WebUI) require an explicit opt-in (e.g. `DSO_ALLOW_UNAUTHENTICATED_LOOPBACK=1`) and log loudly; otherwise refuse to start the REST server without a credential. Small, contained in `internal/server/access_wiring.go` / `auth_gate.go`. Recommended to ship with M1 since it is the same "local caller authorization" problem.

No SSRF filter list is proposed: once callers cannot supply endpoints (item 1), the SSRF surface is the operator's own `dso.yaml`.

## 7. Acceptance criteria for the M1 fix

- `GetSecret` with an unconfigured provider name, or any non-empty `Config`, returns `permission denied` and does not call `LoadProvider` (unit).
- `CheckProviderConnectivity` from a `dso`-group peer is denied; from root it never adds a store entry (unit).
- Two configs under one name yield two entries; `GetProvider` never returns an entry built from a different config digest (unit).
- `GetProvider` twice for `type: env` and `type: file` does not panic; `Shutdown` with native entries does not panic (unit; turns the PE-29 probe into a regression test).
- Linux integration (needs Docker lane + `useradd`): matrix {root, agent uid, `dso` member, other} × each RPC method matches §6 table; a `dso` member cannot read `/etc/shadow` or an agent env var; after an attempted poisoning, the agent's next poll still reaches the configured endpoint (fake Vault containers A and B).

## 8. What was not verified

- End-to-end exploitation over the real socket (needs Linux + Docker lane; macOS has no `SO_PEERCRED` path).
- Per-plugin endpoint override and ambient-credential forwarding (§4.4).
- Secret-value exposure through REST routes in `authOpen` mode (F-E1 leg).

## Appendix A — probe sources (scratchpad, not in repo)

```go
// package providers
s := NewSecretStoreManager(zaptest.NewLogger(t))
prov, _ := s.GetProvider("legacy-oneoff", config.ProviderConfig{Type: "file", Config: map[string]string{"path": dir}})
data, _ := prov.GetSecret("outside-secret-dir")        // returns file content from caller-chosen dir

cfg := config.ProviderConfig{Type: "env"}
_, _ = s.GetProvider("e", cfg); _, _ = s.GetProvider("e", cfg) // second call: nil pointer dereference panic

_, _ = s.GetProvider("vault-prod", config.ProviderConfig{Type: "env"})
_, known := s.Health("vault-prod")                       // true: name now owned by caller-built entry
```
