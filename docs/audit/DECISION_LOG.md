# Decision Log

**Purpose**: Record significant decisions made during audit implementation.  
Future maintainers will understand "why this design" without having to reverse-engineer intent.

---

## Decision Record Template

```markdown
## Decision: [Brief Title]

**Date**: YYYY-MM-DD  
**Issue**: [BUG-1, SEC-2, PRODUCT-1, etc.]  
**Decision Maker**: [Name]  
**Status**: [Proposed / Approved / Implemented / Superseded]

### Context
[Why was this decision needed? What were the constraints?]

### Options Considered
1. **Option A**: [Description]
   - Pros: [list]
   - Cons: [list]
   - Effort: [time estimate]

2. **Option B**: [Description]
   - Pros: [list]
   - Cons: [list]
   - Effort: [time estimate]

3. **Option C**: [Description]
   - Pros: [list]
   - Cons: [list]
   - Effort: [time estimate]

### Decision
**Chosen**: Option B

### Rationale
[Why was this the best choice? What trade-offs were accepted?]

### Consequences
[What becomes easier? What becomes harder? What now needs to be true?]

### Rollback Implications
[If this decision needs to be reversed, what's the cost?]

---
```

---

## Decisions (To Be Populated During Implementation)

### Decision 1: PRODUCT-1 — Merge or Archive feature/web-ui

**Date**: [TBD]  
**Status**: [Proposed]

### Context
The `feature/web-ui` branch contains 143 commits ahead of `main`, including substantial intelligence/governance engines (drift, policy, forecast, apply, compliance). The branch and main have completely diverged.

Options:
1. **Merge**: Rebase and integrate all governance work into main
2. **Archive**: Delete branch, keep only core CLI engine, clean orphaned code

### Decision
**Chosen**: [TBD — Requires stakeholder input]

**If Merge**:
- Rebase 143 commits onto current main
- Resolve conflicts (estimated 10-20 files)
- Extensive regression testing
- Update ROADMAP to reflect new Phase G scope
- New complexity in operations (SQLite, governance APIs)

**If Archive**:
- Delete `feature/web-ui` branch
- Remove orphaned `internal/webui/assets/`
- Update ROADMAP Phase G to reflect "read-only lightweight UI, deferred"
- Reduces maintenance burden, clarifies product roadmap
- Requires tracking any valuable learnings from governance code

---

### Decision 2: SEC-1 — Logging Redaction Strategy

**Date**: 2026-07-28  
**Status**: Implemented

### Context
Redaction engine exists but is unwired. Need to decide implementation approach:

1. **zapcore.Core wrapper** — Intercept all logs at core level
2. **zap.Hook middleware** — Process logs before output
3. **Call-site enforcement** — Require all error logging to call redaction function
4. **Custom logger wrapper** — Thin wrapper around zap that redacts automatically

### Considerations
- **Call-site enforcement** is risky (developers will forget); also 112 existing `zap.Error()` call sites across 21 files would each need auditing
- **Core wrapper** is transparent (all logs automatically redacted, including future code)
- **Hook approach**: rejected — zap's `Hook` is documented for side-effects (metrics/counters), not for mutating written output; `WrapCore` is the documented mechanism for transformation
- **Custom wrapper**: rejected — would require changing the type of every `logger *zap.Logger` struct field across ~15 files, a much larger diff than necessary

### Decision
**Chosen**: Option 1 — `zapcore.Core` wrapper via `zap.WrapCore`, applied inside `observability.NewLogger()` (and the package's `init()`), plus redirecting the 4 direct `zap.NewProduction()`/`zap.NewDevelopment()` bypass sites to that shared factory.

### Rationale
- `zap.WrapCore` is zap's own documented extension point for this exact need
- Wrapping at the Core level covers every field and message automatically, including ones added later via `.With()`/`.Named()`, without touching any of the 112 individual `zap.Error()` call sites
- `pkg/security` (redaction patterns) and `pkg/observability` had zero internal imports before this change — confirmed no import cycle in either direction before adding the dependency

### Critical correction made during implementation
An early draft additionally redacted any field whose *key* contained a sensitive substring (via `security.ShouldLogField`), intended to catch values like `zap.String("db_password", "hunter2ultrasecret")` where the value itself carries no recognizable pattern. Before shipping, a repository-wide grep proved **zero production call sites** actually log a raw credential value through a sensitively-named field — but ~30 call sites across `internal/agent`, `internal/server`, and `internal/injector` legitimately use `zap.String("secret", secretName)` / `zap.String("secret_name", secretName)` to log a secret's *identifier*, not its value. Key-based redaction would have silently destroyed operational and audit visibility (see `internal/audit/audit.go`'s `secret_name` field, documented as "required for compliance") to guard against a threat with no evidence in this codebase. The key-based check was removed; only value-content pattern matching remains. `TestRedaction_DoesNotOverRedactSecretIdentifierFields` locks in the corrected behavior.

### Trade-offs Accepted
- Redaction happens on every log write. Measured (not assumed): a `zap.String` field costs ~19.5x more per call (759ns → 14.8µs, 1 → 70 allocs) than an unwrapped core; a `zap.Any`/Reflect field costs ~31.9µs. Accepted as reasonable given DSO logs on rotation/error events, not a high-throughput request path — but this is a real, quantified cost, not a negligible one
- A separate, older redaction utility (`pkg/observability.Redact`/`ShouldRedactKey`, used only by `internal/core/compose.go`'s `PrintRedactedCompose` for CLI env-var display) was found during review and left untouched — it already unconditionally redacts, is not a zap logging path, and is out of SEC-1's scope. Flagged as a candidate for a future consolidation task, not fixed here.

### Second review round: two genuine defects found before merge (commit f3252c1)
A final pre-merge review (external, checklist-driven) required verifying `redactingCore`'s transparency claims rather than accepting them on faith. This surfaced two real defects in the first commit (0198cc4):

1. **Sampling bypass**: `Check()` tested only `c.Enabled(level)`, never delegating to the wrapped core's own `Check()`. zapcore's sampler (`zapcore.NewSamplerWithOptions`) implements its rate-limit decision inside `Check()`, not `Enabled()` — so this silently disabled sampling for any sampled core wrapped by this decorator. Reproduced concretely (20 identical calls under a 2-per-tick sampler wrote 20/20, not ~2/20) before fixing. Since `zap.NewProductionConfig()` — used by every logger this fix touches — enables sampling by default, this was a live production risk, not a theoretical one. Fixed by delegating to `c.Core.Check(entry, nil)` first (a throwaway CheckedEntry to test "would this be logged") and only registering `c` as the write target if so.
2. **Incomplete field coverage**: `zap.Binary` (`zapcore.BinaryType`) and `zap.Strings`/array fields (`zapcore.ArrayMarshalerType`) were not in the original switch statement and fell through to unredacted pass-through. Confirmed both are used in real production code (`internal/watcher/controller.go` logs `zap.Strings("secrets", ...)` lists). Fixed by treating `BinaryType` like `ByteStringType` (direct byte-content pattern matching) and `ArrayMarshalerType`/`ObjectMarshalerType` like `ReflectType` (JSON round-trip), after verifying `encoding/json` correctly marshals zap's private wrapper types.

Both defects were caught by a review process that demanded concrete reproduction (not just code reading) before accepting a "this works" claim — exactly the discipline that caught the earlier key-based over-redaction mistake.

### Third review round: full field-type audit (commit b92f045)
A mandatory final engineering review (external, exhaustive checklist covering every zap field type constructor) required explicit testing of `Stringer`, `Duration`, `Time`, `Namespace`, `Object`, `Array`, `Inline`, `Skip` — not just the types already handled. This found:

- **Genuine defect (fixed)**: `zapcore.StringerType` — produced by both `zap.Stringer(key, v)` directly AND `zap.Any(key, v)` when `v` implements only `fmt.Stringer` (not `error`) — was unhandled. Reproduced concretely: both paths wrote the raw secret to output. Since `zap.Any()` is already relied upon (and documented in SECURITY.md) to cover struct redaction at 2 real call sites, this contradicted a shipped guarantee. Fixed by treating it like `ErrorType`: call `.String()`, redact, re-wrap as `StringType`.
- **Deferred, documented (not fixed)**: `zapcore.InlineMarshalerType` (`zap.Inline`) — shares `zap.Object`'s `ObjectMarshaler` interface, but converting it to `ReflectType` would change its field-flattening encode behavior in a way not verified safe, and it has zero production call sites. Judged: fixing an untested behavior change for a type nothing uses yet is worse than documenting the gap and revisiting if a real call site appears.
- **Confirmed safe, no change needed**: `Duration`, `Time`, `Namespace`, `Skip` — none carry string-like content that could hold a credential (verified their concrete `f.Interface`/`f.String` representations directly, not assumed).

Additional verification performed this round (all passed, no code changes required):
- Concurrency: 50 goroutines × 50 calls sharing one `redactingCore`, `-race` clean
- Sampling: re-verified across production config, development config (confirmed `Sampling: nil` by default — no sampler to bypass), and the package's global `init()` logger
- Bypass attempts: maps via `zap.Any`, custom `Error()` implementations, `fmt.Errorf` `%w`-wrapped custom errors, multiline messages — all redact correctly
- Confirmed limitation (tested, not assumed): base64/hex-encoded secrets are NOT redacted — regex-based matching cannot see through encoding. This is now explicit, tested, and documented in SECURITY.md rather than an implicit gap

This is the third round where the discipline of "reproduce before trusting" caught something the previous round's (already rigorous) review missed. Each round found genuinely different classes of defect (over-redaction design → sampling/field-type bypass → Stringer-specific type gap), suggesting the review process itself — not any single pass — is what makes this implementation trustworthy.

### Rollback Implications
`git revert` the SEC-1 commits (0198cc4, f3252c1, b92f045, in reverse order); validation = confirm `pkg/security.RedactionPatterns` is referenced only by its own tests again (reverts to the pre-SEC-1 baseline bypass state), `TestRedaction_PreservesZapBehavior/sampling_is_preserved` fails (confirms the sampling fix was reverted), and `TestRedaction_StringerType` fails (confirms the Stringer fix was reverted).

---

### Decision 3: SEC-2 — Plugin Hash Verification Default

**Date**: 2026-07-28  
**Status**: Implemented

### Context
Hash verification is currently optional (env var gated). Need to decide default behavior:

1. **Fail Closed** (Recommended): Require manifest, reject unsigned plugins
2. **Fail Open** (Current): Manifest optional, unsigned plugins allowed

Also discovered during verification: a separate, more mature verification engine (`internal/providers.PluginVerifier`, fail-closed-by-default design, has `CreateHashManifest` tooling) exists but has zero callers anywhere — identical structural pattern to SEC-1's unwired redaction engine.

### Trade-offs
- **Fail Closed**: More secure, but blocks deployment if manifest missing
- **Fail Open**: More backward-compatible, but weaker security posture
- **Wire in existing `PluginVerifier`** vs. **fix `pkg/provider/load.go`'s inline logic**: `PluginVerifier` is more full-featured (mutex-protected registry, manifest generation) but wiring it in crosses a `pkg/`→`internal/` layering boundary and requires touching `internal/providers/store.go`'s call site. Fixing `load.go` directly (where `exec.Command` actually runs) is smaller and keeps the fix at the exact execution boundary

### Recommendation
**Chosen**: Fail Closed, implemented directly in `pkg/provider/load.go` (not by wiring in `PluginVerifier`)

**Rationale**: Security by default — an absent manifest is now a failure, not silent acceptance. Kept in `load.go` because that's the actual point where the subprocess is exec'd; wiring in the separate `PluginVerifier` class would be a larger, cross-package refactor for no functional gain over a contained fix. `PluginVerifier` remains dead code, flagged in the final report as a follow-up candidate for consolidation, not removed (removing unused-but-working code that a future effort might want was judged out of scope for a security fix).

### Design Details
- Default manifest path: `<plugin_dir>/hashes.txt` (no env var required to activate)
- `DSO_PLUGIN_HASH_MANIFEST` changes from an on/off switch to a path override
- New, single escape hatch: `DSO_PLUGIN_SKIP_HASH_VERIFY=1` — explicit, always logs a loud warning when used
- Installer (`internal/bootstrap/provider_plugins.go`) now regenerates the manifest for every plugin present after each `setup` run, so standard installs need no manual manifest management

### Verification (single review round, per the SEC-1-derived stop rule)
- Concurrency: 50 concurrent `enforceHashVerification` calls, 0 races (no shared mutable state — pure filesystem reads)
- Performance: confirmed via `internal/providers/store.go`'s connection cache (`sync.Map`, 10-minute staleness window) that hash verification runs at most every ~10 minutes per provider, not per secret fetch — hashing a plugin binary at that frequency is not a performance concern
- Security: confirmed a pre-existing (not introduced or worsened) check-then-execute TOCTOU window between hashing and exec; documented in SECURITY.md as a residual limitation rather than fixed, since closing it fully requires executing from an already-open fd rather than a re-resolved path — a larger change than this fix's scope
- gosec: one new finding (G306, manifest file permissions) evaluated and deliberately kept at 0644 with a `#nosec` justification — tightening to 0600 would risk breaking hash verification for any deployment where the agent runs as a different user than `setup`, since the manifest holds no secrets (only plugin names + public-binary hashes) and must stay readable across that boundary, matching this codebase's existing 0755 convention for plugin binaries/directory
- Self-review found no new production-impacting defects requiring further investigation before merge.

### Independent final review round (commit [pending], amends 33e12be's scope)
A separate, independent engineering review — deliberately verification-only, explicitly instructed not to redesign or add features — was performed before treating SEC-2 as production-ready. It reproduced one genuine, severe defect the self-review missed:

**Docker deployment completely broken**: `Dockerfile` builds and `COPY`s plugin binaries directly into the image but never invokes `docker dso system setup` — the only path that generates a hash manifest. Every official container deployment would have failed to load any external provider plugin (`enforceHashVerification` rejecting with "cannot open hash manifest"), breaking the primary, most common deployment method entirely, not an edge case. Fixed with a `sha256sum`-based manifest-generation `RUN` step in the Dockerfile's final stage. Verified empirically, not just by code reading: ran an actual `docker build`, extracted the generated manifest via `docker run --entrypoint cat`, and independently cross-checked one hash via `docker run --entrypoint sha256sum` against the manifest entry — confirmed byte-identical.

The review also reproduced (confirming, not discovering) the already-documented residual TOCTOU limitation by tracing the exact two separate `os.Open`/`exec.Command` path resolutions in `load.go`, and verified 5 additional fail-closed edge cases (unreadable/empty/corrupted/malformed-hash/directory-as-manifest) and 3 installer-lifecycle scenarios (reinstall determinism, plugin removal, plugin upgrade) not covered by the original test suite — all passed without needing further code changes.

**Lesson**: the self-review checked the *installer's* manifest generation thoroughly but never checked whether the *other* production plugin-distribution path (the Docker image) also produced one. A review checklist item worth carrying forward to SEC-3 and beyond: **"does this change assume a single distribution path when the codebase has two (bootstrap installer + Docker image)?"**

### Rollback Implications
`git revert`; validation = confirm `DSO_PLUGIN_HASH_MANIFEST` becomes optional again (unset env var + no manifest = successful load, matching pre-SEC-2 behavior). For the Dockerfile fix specifically: confirm a freshly built image no longer contains `/usr/local/lib/dso/plugins/hashes.txt`.

---

### Decision 4: SEC-3 — Plugin Directory Permissions in Container

**Date**: 2026-07-28  
**Status**: Implemented

### Context
Plugin directory (and its contents) was `chown -R dso-user:dso-group` — the same identity the daemon runs as (`USER dso-user`). Verified empirically before designing a fix: a process running as `dso-user` could `rm` and recreate both a plugin binary and the SEC-2 hash manifest (exit code 0 for both), confirming a compromised daemon could substitute a malicious plugin and regenerate the manifest to match it, defeating SEC-2 entirely.

Options considered:
1. **Root-owned, 0555** — Read-only for daemon user, no write bit for anyone including root
2. **Root-owned, 0755** — Read+execute for daemon user (via "other" permission class), no write
3. **Root-owned, 0750** — Writable only by root, readable only by root's group (would need `dso-user` added to a privileged group — more complex, no benefit over option 2)
4. **Mount read-only** — Docker volume mount as read-only at deploy time
5. **No change** — Accept current risk

### Decision
**Chosen**: Root-owned, 0755 (option 2, not the originally-proposed 0555)

### Rationale
- **0555 vs 0755 makes no practical security difference**: root (UID 0) bypasses Unix permission-bit checks entirely regardless of the mode bits set — removing the owner-write bit (0555) doesn't stop root from writing, since root's bypass isn't gated by the file's permission bits at all. The originally-proposed 0555 would have implied a false sense of "even root can't write here," which isn't true. 0755 is the conventional default for directories elsewhere in this Dockerfile and communicates intent accurately: `dso-user` (falling into the "other" permission class, since it's neither the owner `root` nor in `root`'s group) gets read+execute but not write
- **The critical fix is ownership, not permission bits**: if `dso-user` remained the *owner* even under a stricter mode like 0555, a compromised daemon (as `dso-user`) could simply `chmod` the directory back to writable, since `chmod` is a privilege of ownership independent of the file's current mode. Verified empirically: after the fix, `dso-user` attempting `chmod 777` on the directory fails with "Operation not permitted" (confirms ownership change, not just mode bits, is what closes the gap)
- **Rejected "mount read-only at deploy time"**: pushes responsibility to operator configuration; the image should be secure by default without requiring extra flags
- **Rejected "no change"**: leaves a confirmed, empirically-reproduced compromise-amplification path open

### Verification (single review round, per the established stop rule)
- Built the fixed image; confirmed via `docker run --user dso-user` that `rm`, file recreation, and `chmod` on the plugin directory/binaries/manifest all fail (`Permission denied`/`Operation not permitted`)
- Confirmed no functional regression: the daemon (still running as `dso-user`) successfully loads a plugin end-to-end (`LOAD_RESULT: SUCCESS` — real subprocess launch, RPC handshake, hash verification all passing)
- Confirmed bare-metal deployments are unaffected (systemd service already runs as `root`; installer's plugin directory already root-owned)
- Confirmed via exhaustive grep that no runtime code path outside the (separate, root-run) bootstrap installer ever writes to the plugin directory — zero compatibility impact
- No second review round triggered: single round found no new production-impacting defects

### Rollback Implications
`git revert`; validation = confirm `dso-user` regains write access to the plugin directory (reverting to the pre-SEC-3, insecure-but-original behavior).

---

### Decision 5: BUG-1 — Ticker Synchronization Approach

**Date**: 2026-07-28  
**Status**: Implemented

### Context
Tickers map needs synchronization. Options:

1. **Add tickersMu to Agent struct** — Separate mutex for tickers
2. **Reuse existing tickerStopMu** — Use same mutex for both maps
3. **Refactor to goroutine channels** — Eliminate map entirely, use channels

### Decision
**Chosen**: Option 1 — Add `tickersMu sync.Mutex` to Agent struct

### Rationale
- Matches existing pattern (`tickerStopMu` protecting `tickerStopChans`)
- Simple, proven synchronization primitive (sync.Mutex)
- No performance impact (lock duration microseconds)
- Easy to verify with `-race` flag
- Minimal critical sections (only map operations)

### Trade-offs Accepted
- Slightly more struct memory overhead (one mutex field)
- Multiple mutexes (tickersMu + tickerStopMu) instead of one global lock
- Trade-off justified: fine-grained locking enables better scalability

### Implementation Details
- Mutex acquired BEFORE any map operation
- Released immediately after map operation completes
- Slow operations (ticker creation, channel sends) done outside locks
- Lock order consistent (tickerStopMu → tickersMu if both needed)

### Verification
- All 3 concurrent access points protected ✓
- Race detector: 10+ iterations, zero races ✓
- Regression tests: TestTickerMapRace, TestUpdateTickerConcurrency ✓

---

### Decision 6: SEC-4 — Path Validation Symlink Scope

**Date**: 2026-07-28  
**Status**: Implemented

### Context
`pkg/config.IsSafePath` performs only lexical path resolution (`Clean`/`Abs`/`Rel`) with no `filepath.EvalSymlinks` call — confirmed by an already-existing test in this codebase (`TestIsSafePathSymlinkEscapeAttempt`) that created a real escaping symlink and only logged a warning (never asserted failure) when `IsSafePath` accepted it. The function has two branches: `baseDir == ""` (5 of 6 callers — config file, compose file, CLI import/export paths, all invoked directly by an already-privileged operator) and `baseDir != ""` (1 caller — `FileProvider`'s secrets directory, a genuine containment boundary).

### Options
1. **Fix both branches uniformly** — apply symlink resolution regardless of whether `baseDir` is empty
2. **Fix only the `baseDir != ""` branch** — the one genuine privilege/containment boundary
3. **Blindly call `filepath.EvalSymlinks` on the full resolved path** — simplest, but breaks any caller whose target may not exist yet

### Decision
**Chosen**: Option 2, implemented via a `rejectEscapingSymlink` helper covering the full path's parent-directory chain plus an explicit leaf-symlink check — not option 3's naive full-path resolution.

### Rationale
- The `baseDir == ""` callers are all CLI-invoked by an operator who already has whatever filesystem access their own account grants — following a symlink of their own choosing doesn't cross a privilege boundary the way it would for `FileProvider`, where `basePath` represents an admin-configured trust boundary that a *different*, potentially less-trusted request path (the local RPC socket) resolves secret names against
- Option 3 was rejected after tracing `internal/cli/export.go`: it calls `os.Create(safePath)` on an output file that may not exist yet. `filepath.EvalSymlinks` requires every path component to exist, so applying it to the full path would break this legitimate case. Resolving only the *parent* directory (which the operation already requires to exist) avoids this while still catching an intermediate symlinked directory
- The leaf-symlink check (via `os.Lstat`) reuses the exact convention `pkg/provider/load.go`'s `validatePluginPath` already established for plugin binaries — consistent with existing codebase idiom rather than inventing a new pattern

### Verification (single review round, per the established stop rule)
- Turned the existing toothless test into a real assertion; confirmed it fails without the fix and passes with it
- 3 new tests: leaf-is-symlink rejection, new-file-creation tolerance, within-bounds symlink still allowed (proves the fix targets escapes, not symlinks generally)
- Full repo `go test ./...`: all 6 real callers unaffected
- Concurrency: 50×50 concurrent calls, 0 races (pure function, no shared state)
- gosec: 0 new findings from the new code; 1 pre-existing unrelated finding (line 301, Docker socket check, untouched by this diff)

### Rollback Implications
`git revert`; validation = confirm `TestIsSafePathSymlinkEscapeAttempt` reverts to warning-only (no longer asserting failure) and the symlink escape succeeds again.

---

### Decision 7: QUALITY-1 — Proxy Test Coverage Scope

**Date**: 2026-07-28  
**Status**: Implemented

### Context
`internal/proxy` (the TCP reverse proxy behind zero-downtime secret rotation) measured at 5.9% test coverage, with `registry.go` and `router.go` — the actual request-routing core — at 0% across every function. The original audit specifically named these two files as the priority ("treat <10% coverage on a proxy layer as merge-blocking").

### Options
1. **Cover everything to ~100%**, including `ScanAndRegister` (requires a real or heavily-mocked `*client.Client` from the Docker SDK) and every edge branch in `server.go`'s error-retry paths
2. **Cover the named priority files fully, plus what's reasonably testable without new mocking infrastructure** — `registry.go`, `router.go`, `docker_helpers.go` to 100%; `manager.go` and `server.go` using real TCP listeners (no Docker daemon needed for most of their logic)
3. **Cover only the two explicitly-named files** and stop there

### Decision
**Chosen**: Option 2.

### Rationale
- `client.Client` in the Docker SDK version used here is a concrete struct, not an interface — building a fake/mock for `ScanAndRegister` alone would require either a real Docker daemon in CI or a nontrivial HTTP-transport-level fake, disproportionate effort for one thin orchestration function that mostly calls already-tested primitives (`ParseHostPorts`, `extractContainerIP`, `EnsurePort`, `RegisterContainer`)
- `manager.go` and `server.go`'s remaining logic (port binding, connection accept/pipe, backend swap) is fully testable with real `net.Listen` TCP listeners and no Docker dependency at all — there was no reason to leave this untested just because `ScanAndRegister` is harder
- This produced 86.6% overall coverage (100% on the two files the audit specifically named) without inventing disproportionate test infrastructure for the one function that genuinely needs it

### Two findings surfaced, deliberately not fixed here
While reading previously-untested code (a natural consequence of writing tests for it), two pre-existing issues were confirmed via `git diff` to be untouched by this change and were flagged as separate follow-ups rather than fixed inline, per the discipline of keeping a coverage-only change to coverage-only:
1. `Manager.SwapBackend`'s deferred-removal goroutine (`time.Sleep(5*time.Second)`, no context/stop channel) — already identified as BUG-4 in the original audit
2. `Router.Next()`'s `int(n)%len(active)` — a gosec-flagged `uint64`→`int` overflow, confirmed practically unreachable (~9.2 quintillion requests) but a legitimate, cheap fix for a future item

One test (`TestManager_SwapBackend_DeferredRemoval`) deliberately verifies the *correctness* of the deferred removal (it does eventually happen) via polling rather than a blind sleep, without touching or working around the goroutine-lifecycle gap itself — confirming the removal logic works when the process stays alive, which is a distinct question from whether the goroutine survives a *shutdown* mid-drain (BUG-4's actual concern).

### Rollback Implications
Pure test-only addition — no production code changed. "Rollback" here is simply removing the new test files; there is no behavior to revert.

---

### Decision 8: BUG-4 — `Manager.SwapBackend` Drain-Goroutine Lifecycle

**Date**: 2026-07-29  
**Status**: Implemented

### Context
QUALITY-1 confirmed and deliberately deferred this: `SwapBackend`'s deferred-removal step (waits 5s, then removes the drained backend from the registry) ran in a bare `go func() { time.Sleep(5*time.Second); ... }()` with no `context` or stop channel tied to `Manager.Stop`. `Stop` only closed the server's listeners — it had no way to know these goroutines existed, let alone cancel or wait for them, so one could keep running and mutating `m.registry` for up to 5 seconds after `Stop` returned.

### Options
1. **Bound the goroutine to a Manager-lifetime context + WaitGroup**, cancel and wait for it inside `Stop`
2. **Track pending removals in a slice/map and cancel via a shared "stopped" flag checked in a loop** — functionally equivalent to option 1 but reinvents what `context.Context` already provides
3. **Leave it unfixed** and only document the limitation

### Decision
**Chosen**: Option 1.

### Rationale
- `context.Context` + `sync.WaitGroup` is the idiomatic Go pattern for exactly this shape of problem (bound a background goroutine's lifetime to its owner's, let the owner wait for clean shutdown) — no need to hand-roll a flag/polling mechanism
- Racing `time.After(5*time.Second)` against `ctx.Done()` in a `select` means the fix costs nothing in the common case (Manager stays up, the timer fires normally, behavior is unchanged) and only changes behavior during shutdown, which is exactly the gap being closed
- On cancellation the goroutine skips the registry removal rather than trying to still perform it — correct, because `Manager.Stop` being called means the whole proxy (and its registry) is being torn down; there is nothing left to usefully clean up
- No public API changed: `NewManager`, `SwapBackend`, and `Stop` all keep their existing signatures

### Verification (single review round, per the established stop rule)
- Reproduced empirically before fixing: reverted `manager.go` only (kept the new test), ran `TestManager_Stop_TerminatesSwapBackendDrainGoroutine` — failed as expected (`goroutine count still elevated 300ms after Stop returned`)
- Restored the fix, same test passes, and now returns in ~0ms instead of needing the 300ms settle window
- Full `internal/proxy` suite re-run under `-race`: clean, coverage 86.6% → 86.9%
- `go build ./...`, `gofmt`, `go vet`, `golangci-lint`, `gosec`, `govulncheck` all run against the changed files: zero new findings introduced by this diff (the pre-existing `router.go` G115 finding, `server.go` G104/errcheck findings, and stdlib/dependency CVEs from `govulncheck` are all unrelated to `manager.go` and unchanged by this change)

### Rollback Implications
Single-file production change (`internal/proxy/manager.go`) plus a test-only addition. Revert is a straightforward single-commit revert; no data, schema, or API compatibility impact.

---

### Decision 9: AUDIT-1 — Wiring `internal/audit` Into a Real Call Site

**Date**: 2026-07-29  
**Status**: Implemented

### Context
The comprehensive review (`docs/audit/COMPREHENSIVE_REVIEW.md`, Phase 3/7) found `internal/audit` — a fully built, SEC-1-hardened, unit-tested compliance logger — had zero production callers. This was flagged as worse than ordinary dead code: `AuditEvent`'s own doc comment says "required for compliance," and the SEC-1 changelog entry documents hardening this exact package, so a reader would reasonably assume secret access is audited when it isn't.

### Options
1. **Delete `internal/audit`** — remove the unused feature entirely
2. **Wire it into every secret-touching call site** (rotation, injection, CLI `secret get/set`, agent fetch) in one pass
3. **Wire it into the single highest-value, lowest-risk call site** — the agent's `GetSecret` RPC handler, which every secret fetch passes through regardless of caller — and explicitly scope out the rest as follow-ups

### Decision
**Chosen**: Option 3.

### Rationale
- The user only asked "what is this for, and wire it in if needed" — not to redesign the audit surface across the whole codebase. `AgentServer.GetSecret` (`internal/agent/server.go`) is the one RPC method every secret-fetch path (`fetch`, `inject`, `apply`, `up`, `compose`, `sync`) already funnels through, so wiring it there gets the majority of compliance value from a single, well-tested, contained change rather than a multi-package sweep
- Deleting it (Option 1) would have been defensible too, but the feature is well-built and the "required for compliance" framing in its own doc comment suggests it was intentional, not accidental scaffolding — wiring it in is the more conservative choice given ambiguity about intent
- Full multi-site wiring (Option 2) would have touched `internal/rotation`, `internal/watcher`, and `internal/cli` in the same change — a materially larger diff spanning packages with different data shapes available (e.g., rotation events don't naturally carry a "provider" the same way a fetch does), better done as its own separately-reviewed change
- **Known, documented limitation accepted rather than solved here**: the `user` field is recorded as the fixed value `"agent"`. Real per-caller attribution requires the connecting peer's authenticated OS UID (already captured at socket-accept time via SEC-C2's `readPeerIdentity`) to be threaded through `net/rpc`'s per-call dispatch, which doesn't naturally support passing connection-scoped data into method calls. Solving this properly means changing the RPC transport (a custom `ServerCodec` or moving off `net/rpc`'s built-in dispatch) — out of proportion for "wire in an existing `Log()` call." Tracked as a follow-up rather than silently left unmentioned

### Verification (single review round, per the established stop rule)
- Reproduced empirically before fixing: reverted `server.go` only (kept the new tests), confirmed both new tests fail against pre-fix code (`expected audit log to contain "event":"secret_fetch", got: ` — empty, because nothing was logged)
- Restored the fix, both tests pass; confirmed the audit record never contains the fetched secret's plaintext value
- Full `internal/agent` suite re-run under `-race`: clean. `internal/audit`'s own existing test suite re-run unchanged and still passing
- `go build ./...`, `gofmt`, `go vet`, `golangci-lint`, `gosec` run against the changed files: zero new findings introduced (the pre-existing G118/G304/G104 findings elsewhere in `server.go`/`trigger.go`/`agent.go` are all outside this diff's line ranges, confirmed via `git diff`)

### Rollback Implications
Two-file production change (`internal/agent/server.go` adds the wiring, no other production files touched) plus a new test file. Revert is a straightforward single-commit revert; `internal/audit` itself is unchanged, so reverting only removes the call sites, not the underlying (already-tested) logging capability.

---

### Decision 10: CLEAN-1 — Dead Code Removal (`internal/runtime`, `plugin_verifier.go`, `internal/webui`)

**Date**: 2026-07-29  
**Status**: Implemented

### Context
The comprehensive review flagged three items as delete candidates: `internal/runtime` (test-only, zero importers), `internal/providers/plugin_verifier.go` (superseded by SEC-2's `pkg/provider/load.go`, zero callers, previously flagged as `task_9228adcc` and left unactioned), and `internal/webui` (believed to be an orphaned Next.js build artifact). The user asked to proceed on the first two and explicitly asked to investigate the third rather than delete it blind, since `feature/web-ui` also touches web UI code.

### Verification (re-run before touching anything, per the standing discipline)
- `internal/runtime`: re-confirmed via `grep -rl "internal/runtime" --include="*.go" .` — zero results. Still dead
- `plugin_verifier.go`: a `grep` for "plugin_verifier" initially surfaced a hit in `internal/server/rest.go:242` — investigated before deleting, and found it to be a **stale comment** referencing a nonexistent path (`pkg/provider/plugin_verifier.go`, never a real file) rather than an actual caller. `PluginVerifier`'s exported methods have zero real Go-code callers outside its own package/test. Still dead
- `internal/webui`: this is where the investigation changed the plan. `git log main -- internal/webui` returns **empty** — main's own commit history has never touched this path. The three commits that built a full web UI (`embed.go`, `proxy.go`, `server.go`, `server_test.go`, ~150 assets) exist only on `feature/web-ui` (and two other unrelated branches, `advanced-platform`/`intelligence-pack`) — `git merge-base --is-ancestor` confirms none of them are ancestors of `main`. The single file physically present in `main`'s working tree (`internal/webui/assets/_next/static/chunks/app/secrets/page-5a9abdf1eec286d0.js`) is **not tracked by git at all**: `git ls-tree -r main` shows nothing for this path, and `.gitignore:84` excludes `internal/webui/` outright. The filename doesn't even match anything in `feature/web-ui`'s current tree (different content hash, different page). Conclusion: this was stray, gitignored local working-tree cruft — most likely left behind by a local build or an unclean branch switch at some point — with zero relationship to `feature/web-ui`'s real (and currently unmerged) web UI work

### Decision
Deleted `internal/runtime` and `internal/providers/plugin_verifier.go`/`plugin_verifier_test.go` as tracked, committed changes. Corrected the stale comment in `rest.go` that referenced the deleted file's (already-wrong) old path. Removed the stray `internal/webui` file from the working tree directly (`rm -rf`, not `git rm` — git was never tracking it, so there is no commit for this specific removal and no effect on `feature/web-ui`'s eventual merge). `.gitignore`'s `internal/webui/` rule was deliberately left in place, since it's still relevant to `feature/web-ui`'s unresolved merge/archive decision (PRODUCT-1).

### Rationale
- Nothing here required a design trade-off — both `internal/runtime` and `plugin_verifier.go` were unambiguously dead (zero importers, confirmed twice now across two review passes)
- The `internal/webui` question wasn't "is this safe to delete" in isolation — it was "does deleting this interact with the still-open `feature/web-ui` branch decision." It doesn't: main's copy was never part of main's history, so removing it doesn't touch, complicate, or foreclose PRODUCT-1's eventual merge/archive decision on `feature/web-ui` in any way
- Left the `plugin_verifier.go` deletion's follow-up task (`task_9228adcc`) resolved by this change rather than leaving a stale chip pointing at now-deleted files

### Verification
- `go build ./...`: clean
- `go vet ./...`: clean
- Full `go test ./... -short`: all packages pass, `internal/runtime` and `internal/providers/plugin_verifier*` no longer appear in the package list
- `gofmt`, `golangci-lint`, `gosec` on the touched packages (`internal/providers`, `internal/server`): zero new findings; all pre-existing findings elsewhere in those packages confirmed unrelated to this diff

### Rollback Implications
`internal/runtime`/`plugin_verifier.go` deletions are a normal single-commit revert (git history preserves the files). The `internal/webui` cleanup has no git footprint to roll back — it never was one.

---

### Decision 11: SEC-5 — Making `gosec`/`govulncheck` Blocking, Enabling SBOM

**Date**: 2026-07-29  
**Status**: Implemented

### Context
CI's `gosec` ran with `-no-fail` and `govulncheck` ran with `continue-on-error: true` — both flagged in the comprehensive review as a real gap: DSO's two automated security scanners could not, in practice, fail a build. Before touching CI config, ran both scanners repo-wide to measure the actual gap: 84 pre-existing `gosec` findings and 22 reachable `govulncheck` vulnerabilities.

### Options considered
1. **Flip the flags off immediately** — would have made CI permanently red on unrelated pre-existing findings, including 3 `govulncheck` findings in `docker/docker` with no available fix (`Fixed in: N/A`) — not actually achievable, and a bad first impression of "blocking" security tooling
2. **Blanket rule/path exclusions** (e.g., `-exclude=G204,G304,...` or excluding whole packages) — technically makes the gate pass, but also silently exempts *future* findings of those types, defeating the point of making the gate blocking in the first place
3. **Triage every finding individually** (fix, tighten, or suppress with a specific, verified justification) and only then flip the flags, plus build an allowlist mechanism for the handful of genuinely unfixable `govulncheck` findings

### Decision
**Chosen**: Option 3, for both tools.

### Rationale
- **`govulncheck`**: re-scanning showed 19 of 22 findings were fixable by version bumps alone (14 Go-stdlib CVEs fixable by bumping the toolchain to `go1.25.12`; 4 dependency CVEs fixable via `go get`) — the "unfixable" framing in the old CI comment (citing a single CVE, `GO-2026-5746`) was stale; most of the backlog was never actually unfixable, just never revisited. Bumped the toolchain and 4 dependencies, verified the full test suite still passes under `-race`, and built a small `jq`-based CI script (`.govulncheck-allowlist.txt` + inline logic in `ci.yml`) that only allowlists the 3 vulnerabilities confirmed to have no upstream fix (`GO-2026-4883`, `GO-2026-4887`, `GO-2026-5668`, all `github.com/docker/docker`), scoped to *reachable* findings only (govulncheck's own `trace[0].function` field) so a genuinely new, fixable vulnerability still fails the build
- **`gosec`**: rejected blanket exclusion specifically because several of the 84 findings' rule categories (`G304` file-inclusion, `G204` subprocess-exec) are exactly the categories most worth catching in a secrets-management tool — excluding them wholesale would blind the gate to the exact class of bug it exists to catch. Went finding-by-finding instead: 22 unchecked-error discards fixed with the idiomatic `_ = ` (satisfies gosec and `golangci-lint`'s `errcheck` simultaneously), a real integer-overflow panic in `Router.Next()` fixed properly (not suppressed) with a reproduction test proving the pre-fix code actually panics, an MD5→SHA-256 swap for a non-security hash (removes ambiguity, not a real vulnerability fix), 3 permission-bit tightenings where no broader-access requirement existed, and 57 suppressions — each with an inline comment naming the specific reason (already-validated by SEC-2/SEC-4, sanitized by an existing function, a CLI tool operating within the invoking user's own permissions, a Unix-socket dial structurally incapable of SSRF, etc.), not a generic "false positive" comment
- **Version-pinning discipline**: local triage initially used `gosec@latest` (v2.28.0); before declaring the gate green, re-verified against CI's actually-pinned `v2.25.0`, which caught 5 *additional* findings (`uintptr`→`int` file-descriptor conversions) the newer version doesn't flag — confirming the gate is checked against what CI will actually run, not just what was convenient locally
- **SBOM**: this was already fully scaffolded (`syft` installed in `release.yml`) and only commented out in `.goreleaser.yml` — enabling it was a one-line-block change, not new infrastructure

### Verification (single review round, per the established stop rule)
- `go build ./...`, `go vet ./...`: clean
- Full `go test ./... -race -short`: all packages pass (including a new regression test, `TestRouter_Next_CounterNearOverflow`, verified failing against the pre-fix code with a real panic — `index out of range [-2]` — before being restored)
- `gofmt` (using the toolchain-matched binary, not a stray system one): clean repo-wide after reformatting ~25 files affected by the toolchain bump's minor comment-alignment shift
- `golangci-lint run ./...`: 112 issues, verified via a side-by-side worktree comparison against the pre-session `HEAD` (111 issues) that every issue is pre-existing and unrelated to this diff, except 2 new `govet` findings — both in a vendored third-party file (`web/node_modules/flatted/...`), an artifact of the toolchain bump surfacing a newer deprecation-inlining suggestion in code this repo doesn't own; flagged as a separate follow-up (exclude `web/node_modules/` from linting) rather than fixed here
- `gosec ./...`: 0 issues, verified against both `@latest` and CI's exact pinned `v2.25.0`
- `govulncheck`: 3 remaining findings, all present in `.govulncheck-allowlist.txt`; the CI gating script itself was tested both positively (passes with the current allowlist) and negatively (fails when an entry is removed, confirmed via a temp-file test)
- `goreleaser check`: SBOM config valid; 2 unrelated, pre-existing deprecation warnings noted, not fixed (out of scope)

### Rollback Implications
The toolchain/dependency bump is the highest-blast-radius part of this change (touches `go.mod`/`go.sum` and, transitively, every package) — mitigated by running the full test suite under `-race` before and after. `#nosec` suppressions are all inline comments (revertible per-file). The CI workflow and `.goreleaser.yml` changes are config-only. A full revert would be a single commit revert; a partial revert (e.g., keeping the toolchain bump but reverting the CI blocking behavior) is also straightforward since the two are independently committable concerns.

---

### Decision 12: AUDIT-2 — Threading the Real Peer Identity into the Audit Log

**Date**: 2026-07-29  
**Status**: Implemented

### Context
AUDIT-1 wired `internal/audit.Log` into `AgentServer.GetSecret` but recorded every caller as a fixed `"agent"` placeholder, explicitly scoped out as a follow-up: the connecting peer's OS UID is already authenticated per-connection at socket-accept time (SEC-C2's `readPeerIdentity`), but `net/rpc`'s dispatch model gives `GetSecret(req, resp)` no way to see which connection a given call arrived on.

### Options
1. **A custom `rpc.ServerCodec`** wrapping the default gob codec, stashing the peer identity in a side-channel (e.g. a `sync.Map` keyed by something derivable inside the method call)
2. **Per-connection `*rpc.Server` + per-connection receiver value** — since `rpc.ServeConn` already runs in its own goroutine per accepted connection with the peer identity already in scope at that point, give each connection its own lightweight `*rpc.Server` and register a small wrapper value (embedding the shared `*AgentServer`, carrying this connection's resolved identity) on it instead of using the process-wide `rpc.RegisterName`/`rpc.ServeConn` convenience functions
3. **Move off `net/rpc` entirely** (e.g., to gRPC or a hand-rolled protocol with explicit per-call context) to get proper context propagation

### Decision
**Chosen**: Option 2.

### Rationale
- Option 3 would be a full transport rewrite touching every RPC call site in the codebase (`internal/injector.AgentClient` and everything built on it) for a single field's attribution — wildly disproportionate
- Option 1 (custom codec + side-channel map) solves the same problem as option 2 but with more moving parts (a map to keep in sync with connection lifecycle, an extra layer of indirection to keep straight) for no additional benefit — `net/rpc`'s own documented pattern for "receiver needs per-connection state" is exactly a per-connection `*rpc.Server` + per-connection receiver value, which is option 2
- `agentConn` embeds `*AgentServer` rather than duplicating its method set, so `GetEvents` (and any RPC method added later) is promoted unchanged with zero extra code; only `GetSecret` needed overriding, keeping the diff minimal
- The exported `AgentServer.GetSecret` (still reachable directly, not via RPC, from `StartDriverServer`'s HTTP path) needed *some* placeholder since that path has no peer-credential concept at all — changed it from the old shared `"agent"` to `"docker-secret-driver"`, a real and distinct identifier, rather than leaving a second ambiguous placeholder in place
- A UID that doesn't resolve to a local account (e.g. NSS/LDAP-only accounts under the CGO-disabled build, per `peerAuthorized`'s existing comment on this exact caveat) degrades to a `"uid:<n>"` audit value rather than failing the request — audit attribution should never block an already-authorized secret fetch

### Verification (single review round, per the established stop rule)
- Reproduced empirically before fixing: stashed only `server.go`/`peercred.go` (reverting fully to committed `HEAD`, which predates AUDIT-1 entirely), confirmed both new tests fail — the peer-identity test asserted this sandbox's real resolved username and got an empty audit log, the HTTP-driver test asserted `"docker-secret-driver"` and got nothing
- Restored the fix, both tests pass; `TestStartSocketServer_AuditLogsRealPeerIdentity` dials a real `StartSocketServer` over an actual Unix socket and makes a real `net/rpc` call — not a mocked/direct method call — so it exercises the exact dispatch path a real client uses
- Full `internal/agent` suite (including the pre-existing AUDIT-1 tests, unaffected) re-run under `-race`: clean — the new per-connection `*rpc.Server` allocation happens inside the same per-connection goroutine that already existed, no new shared state introduced
- `go build ./...`, `go vet ./...`, `gofmt`, `golangci-lint`, `gosec`: one new `staticcheck` style suggestion (`QF1008`, redundant embedded-field selector) found and fixed immediately (`c.AgentServer.getSecret(...)` → `c.getSecret(...)`, relying on promotion); zero other new findings

### Rollback Implications
Two-file production change (`internal/agent/server.go`, `internal/agent/peercred.go`) plus new tests. A revert restores the `"agent"` placeholder behavior exactly as AUDIT-1 shipped it — no data or API compatibility impact; `internal/injector.AgentClient` and all other RPC callers are unaffected since `Agent.GetSecret`/`Agent.GetEvents`'s wire-visible method names and argument/reply types are unchanged.

---

### Decision 13: AUDIT-3 — Extending the Audit Trail to Rotation Events

**Date**: 2026-07-29  
**Status**: Implemented

### Context
AUDIT-1 wired `internal/audit.Log` into secret *access* (`AgentServer.GetSecret`) and explicitly scoped out rotation-completion events as a separate follow-up. The comprehensive review's Phase 3/7 framing wanted the audit trail to also cover "secret X was rotated into container Y, success/failure" — the other half of what a compliance audit trail for a secrets-rotation tool should record.

### Investigation
Traced the call chain from where a secret change is first detected to where a rotation's outcome is finally known:
- `internal/agent/trigger.go`'s secret-change-detection loop has `providerName`/`secretName` cleanly in scope, but only calls `Reloader.TriggerReload(ctx, secretName)` and checks whether *triggering* succeeded — not whether the actual container swap succeeded, which happens asynchronously
- `internal/watcher/controller.go`'s `TriggerReload`, specifically its "rolling" strategy branch (the actual blue-green swap, delegating to `internal/rotation.RollingStrategy.Execute`), is the only place that both triggers the rotation *and* eventually learns its real, final outcome (line ~729 in the current file: `err := rs.Execute(...)`) — confirming the comprehensive review's own hypothesis that this file's reconciliation loop was the right candidate

### Decision
Wired `audit.Log` into the "rolling" strategy's goroutine in `TriggerReload`, immediately after `rs.Execute` returns, for both the success and failure branches. Added `resolveSecretProvider` to look up the provider name for the given secret the same way the surrounding code already does it (mirroring, not duplicating, the existing inline fallback logic). Deliberately left the "restart" and "signal" strategies unaudited — they don't call into `internal/rotation` at all, so auditing them is a different, separate question (not "did the blue-green rotation succeed") better left for its own follow-up if wanted.

### Rationale
- `internal/watcher/controller.go` was the right choke point, not `internal/agent/trigger.go`, precisely because it's the only place where the *actual* outcome (not just "was a rotation triggered") is known — auditing at the trigger.go layer would have produced a compliance record that could say "success" for a rotation that then failed asynchronously
- Scoping to "rolling" only (not "restart"/"signal") mirrors AUDIT-1's own discipline of picking the single clearest, most clearly in-scope choke point rather than trying to cover every adjacent code path in one change
- `resolveSecretProvider` reuses the exact resolution order (`sec.Provider`, else the sole configured provider) the surrounding rotation code already relies on for building env vars, so the audit record's `provider` field can never disagree with what the rotation itself actually used

### Verification (single review round, per the established stop rule)
- Reproduced empirically before fixing: stashed only `controller.go`, confirmed `TestTriggerReload_RollingRotation_AuditLogsFailure` fails against the pre-fix code (`timed out waiting for audit log to contain "event":"rotate"; got: `), restored, confirmed it passes
- `TestTriggerReload_NonRollingStrategy_NoRotationAuditLog` confirms the "signal" strategy scope decision holds (no rotation audit event emitted)
- **A genuine data race was found and fixed during this work — in the new test helper, not production code.** `go test -race` caught the test's own audit-log buffer (a bare `bytes.Buffer`) being read by the polling goroutine while the rotation goroutine wrote to it concurrently. Fixed by introducing a small mutex-guarded `syncBuffer` type in the test file. Documented here rather than silently fixed because it's a useful reminder that `-race` earns its keep even on test infrastructure, not just production code
- Full `internal/watcher` suite re-run under `-race` (including 3x repeats of the two new tests specifically): clean
- `go build ./...`, `go vet ./...`, `gofmt`, `golangci-lint`, `gosec`: zero new findings (confirmed via `git diff` line-range comparison against golangci-lint's output — all 9 pre-existing findings in this package are outside this diff's touched lines)

### Rollback Implications
Two-file change (`internal/watcher/controller.go` production code, `internal/watcher/controller_audit_test.go` new tests). No API/wire compatibility impact — `TriggerReload`'s signature and behavior for its actual (non-audit) job are unchanged; a revert simply stops emitting the new `"rotate"` audit events.

---

### Decision 14: CLEAN-3 — Excluding Vendored `web/node_modules` from `golangci-lint`/`gosec`

**Date**: 2026-07-29  
**Status**: Implemented

### Context
SEC-5's Go toolchain bump (`go1.25.6` → `go1.25.12`) surfaced 2 new `govet` findings in `web/node_modules/flatted/golang/pkg/flatted/flatted.go` — an npm package's bundled Go binding, not DSO's own code. Flagged at the time as a config gap rather than fixed inline, since it wasn't part of SEC-5's actual scope.

### Investigation
`run.exclude-dirs` and `issues.exclude-dirs` — the config keys most commonly cited for this in older golangci-lint documentation — were tried first and empirically confirmed to have **no effect** under the exact pinned CI version (v2.12.2, also the version installed locally): the vendored file kept appearing in output under both. `linters.exclusions.paths` — golangci-lint v2's restructured config schema — was tried third and confirmed to work, verified against the same pinned binary before editing the real config file.

### Decision
Added `linters.exclusions.paths: [web/node_modules]` to `.golangci.yml`. Added `-exclude-dir=web` to `gosec`'s CI invocation (a separate CLI flag, since `gosec` doesn't share a config file with `golangci-lint`), kept in sync so both tools apply the same boundary.

### Rationale
- Verifying against the schema empirically (rather than trusting documentation or memory of an older config version) mattered here specifically because a config change that *looks* right but silently does nothing is worse than no change at all — it would have shipped a false sense of the gap being closed
- `-exclude-dir` (gosec) vs. `linters.exclusions.paths` (golangci-lint) are necessarily different mechanisms since the two tools don't share configuration; keeping them documented as "kept in sync" in both the CI comment and this entry is the cheap insurance against them drifting apart on a future change

### Verification
- `golangci-lint run ./...` (real config): 110 issues, matching the expected pre-existing baseline exactly, zero `govet` findings, zero mentions of `node_modules`/`flatted`
- `gosec -exclude-dir=web ./...` (matching CI's exact invocation): file count 166→165, 0 issues, zero mentions of `node_modules`/`flatted`
- `go build ./...`: clean (config-only change, no Go source touched)
- Both YAML files validated for syntax

### Rollback Implications
Two config files only (`.golangci.yml`, `.github/workflows/ci.yml`); no Go source touched. Trivial single-commit revert if ever needed.

---

### Decision 15: SEC-6 — Allow-List Secret File Names to Block Shell Injection

**Date**: 2026-07-29  
**Status**: Implemented

### Context
`docs/audit/2026-07-29-fresh-audit.md` (fresh Go-backend audit) found that `internal/injector/inject.go`'s `injectOneFile` interpolates a secret file name unquoted into a shell command run via `docker exec /bin/sh -c`. The name is derived (`internal/resolver/resolve.go:232`) from a compose file's service key, which is untrusted input with no character restrictions from the YAML parser. The existing sanitization (`filepath.Base` + rejecting empty/`.`/`/`) only defended against path traversal, not shell metacharacters — a service key like `app; touch /run/secrets/dso/pwned #` would execute arbitrary commands inside the target container.

### Options Considered
1. **Allow-list regex on the file name** (`^[A-Za-z0-9._-]+$`), enforced in `injectOneFile` right before the command is built.
   - Pros: single choke point at the actual shell boundary, catches the issue regardless of which caller/resolver logic changes upstream; small diff; consistent with the existing validation style in the same function.
   - Cons: none identified — legitimate compose service names and secret paths are already restricted to this character set by Docker Compose's own naming rules, so no valid use case is broken.
   - Effort: <1 hour.

2. **Sanitize/reject at the source** (`internal/resolver/resolve.go`, where `serviceName` is first read from YAML).
   - Pros: rejects bad input earlier, closer to where it originates.
   - Cons: doesn't protect against a different future caller of `InjectFiles` that isn't `resolve.go`; `injectOneFile` would still contain the same latent bug. Also a larger diff touching the resolver's parsing path.
   - Effort: ~1-2 hours.

3. **Avoid shell interpolation entirely** — pass the file name as an argument/env var to a static script instead of splicing it into the command string.
   - Pros: eliminates the class of bug entirely, not just this instance.
   - Cons: larger refactor of `buildInjectCmd`/`injectOneFile`'s exec plumbing; changes the shape of the `docker exec` call in ways that need broader re-testing; out of proportion to the fix needed right now.
   - Effort: ~1 day.

### Decision
Chose Option 1: added a `validFileName` regexp (`^[A-Za-z0-9._-]+$`) in `internal/injector/inject.go`, checked immediately after the existing path-traversal checks in `injectOneFile`, rejecting any name that doesn't match before `buildInjectCmd` is called.

### Rationale
- The shell command is built and executed in exactly one place (`injectOneFile`); defending there means the fix holds even if `internal/resolver` or a future caller changes how file names are derived.
- An allow-list (reject-by-default) is safer than a denylist of shell metacharacters, which is easy to get wrong (e.g. missing a metacharacter, or interaction with locale/encoding edge cases).
- The character set matches what's already legitimate for a secret file name (`filepath.Base` + prior traversal checks already assumed a "plain" name); no behavior change for any valid input.

### Verification
- New regression test `TestInjectOneFile_ShellInjection` in `internal/injector/inject_test.go` — covers `;`, backtick, `$()`, `|`, `&`, space, newline, and `'; rm -rf / #` payloads, all now rejected with `invalid secret file name`.
- Existing `TestInjectOneFile_PathTraversal` and `TestInjectOneFile_ValidFilename` still pass — traversal rejection and legitimate filenames are unaffected.
- `go build ./...`: clean.
- `go vet ./...`: clean.
- `go test -race ./internal/injector/... ./internal/resolver/... ./internal/agent/...`: all pass, 0 races.

### Rollback Implications
Single file touched for the fix (`internal/injector/inject.go`), single file for the test (`internal/injector/inject_test.go`). No API/behavior change for legitimate compose service names. Trivial single-commit revert if ever needed — reverting only restores the pre-existing (already-tracked) vulnerability, so a revert should not happen without a replacement fix.

---

### Decision 16: REL-1 — Defer Rotation-Complete State Until Async Work Actually Finishes

**Date**: 2026-07-29  
**Status**: Implemented

### Context
`docs/audit/2026-07-29-fresh-audit.md` found that `internal/watcher/controller.go`'s `TriggerReload` launches the real rotation work (rolling swap, restart, or compose-project restart) in detached `go func(...)` goroutines and returns before they finish. The caller, `internal/agent/trigger.go`, called `StateTracker.CompleteRotation` immediately after `TriggerReload` returned — so crash-recovery state was marked "complete" while the actual container swap could still be running (or about to fail) in the background, with no way to correct the state afterward.

### Options Considered
1. **Make `TriggerReload` block until all rotation goroutines finish.**
   - Pros: simplest mental model — state is always accurate by the time the function returns.
   - Cons: changes `TriggerReload`'s fire-and-forget timing contract; `trigger.go`'s existing long-lived context comment explicitly relies on `TriggerReload` returning quickly so the goroutines can keep running past a short-lived caller scope. Blocking here risks serializing rotations that were designed to overlap and could reintroduce timeout/deadlock surface.
   - Effort: ~2-3 hours, but with more behavioral risk.

2. **Add an `onComplete func(error)` callback parameter, invoked once after all of `TriggerReload`'s detached goroutines for that call finish**, aggregating the first error.
   - Pros: preserves the existing non-blocking return; state is updated exactly when the real outcome is known, no matter which strategy (rolling/restart/compose) ran; single choke point (a `sync.WaitGroup` + mutex-guarded first-error) inside `TriggerReload` itself, so every current and future goroutine launch site is covered as long as it's wired into the same WaitGroup.
   - Cons: `TriggerReload`'s signature changes (one new parameter); every call site needs updating (only one production call site and two test call sites existed, per `grep`).
   - Effort: ~1-2 hours.

3. **Return a `*sync.WaitGroup` (or similar handle) from `TriggerReload` for the caller to `Wait()` on before deciding completion.**
   - Pros: no callback indirection.
   - Cons: leaks the "how many goroutines were launched" internal detail into the caller's control flow, and still requires the caller to separately track *which* of those goroutines failed (aggregation would have to happen in `trigger.go` instead of at the source), which is a worse separation of concerns than Option 2.
   - Effort: ~1-2 hours.

### Decision
Chose Option 2. Added `onComplete func(err error)` as `TriggerReload`'s third parameter. Internally, a `sync.WaitGroup` (`asyncWG`) is incremented before every goroutine launch (rolling, restart, and compose-restart) and decremented via `defer asyncWG.Done()`; each goroutine's terminal error (success or failure, at every existing return point) is recorded into a mutex-guarded `asyncFirstErr` via a `recordAsyncErr`/`finish` helper. After the synchronous part of `TriggerReload` completes, a small waiter goroutine calls `asyncWG.Wait()` then invokes `onComplete(asyncFirstErr)` exactly once. `internal/agent/trigger.go` moved its `StateTracker.CompleteRotation`/`MarkRollback` logic out of the code that ran immediately after `TriggerReload` returned and into this `onComplete` callback.

### Rationale
- Preserves `TriggerReload`'s existing timing contract (callers already document why it must return quickly — that comment in `trigger.go` predates this fix and stays correct), so this fix doesn't reintroduce whatever problem prompted the fire-and-forget design in the first place.
- Aggregating errors inside `TriggerReload` (where the goroutines actually run) rather than in the caller keeps the "did the real work succeed" logic next to the code that does the real work — the caller only needs to react to a final pass/fail, not reconstruct it from partial signals.
- Synchronous strategies (`signal`, and the "no strategy matched" fallback) are unaffected — they complete before `TriggerReload` returns already, so they were never part of this bug and don't need to go through the callback.

### Verification
- New regression test `TestTriggerReload_OnComplete_FiresAfterAsyncRotationFinishes` in `internal/watcher/controller_audit_test.go` — forces a rolling-rotation failure (mock daemon returns 500 on inspect) and asserts `onComplete` fires strictly after `TriggerReload` returns, exactly once, with a non-nil error (proving it reflects the async goroutine's real outcome, not a false success recorded at return time).
- Existing `TestTriggerReload_RollingRotation_AuditLogsFailure` and `TestTriggerReload_NonRollingStrategy_NoRotationAuditLog` updated for the new signature (passing `nil` where no callback is needed) and still pass unchanged otherwise.
- `go build ./...`, `go vet ./...`, `gofmt -l`: clean.
- `go test -race ./internal/watcher/... ./internal/agent/...`: all pass, 0 races.
- Full `go test ./...`: all packages pass.

### Rollback Implications
Three files touched: `internal/watcher/controller.go` (fix), `internal/agent/trigger.go` (caller update), `internal/watcher/controller_audit_test.go` (new + updated tests). The only other call sites needing a signature update were the two existing tests in the same file. Reverting restores the pre-existing (already-tracked) state-tracking gap, so — as with SEC-6 — a revert should not happen without a replacement fix.

---

### Decision 17: SEC-7 — Reject Privileged/Out-of-Range Host Ports in `ParseHostPorts`

**Date**: 2026-07-29  
**Status**: Implemented (partial mitigation — see Rationale)

### Context
`docs/audit/2026-07-29-fresh-audit.md` found that any running container can set the `dso.host_ports` label (read in `internal/proxy/manager.go` and `internal/watcher/controller.go`) and DSO will open a listener bound to all interfaces and proxy to that container, with no validation beyond the format being parseable — no port-range check, no privileged-port restriction, no check that the container is one DSO's operator actually intended to expose.

### Options Considered
1. **Full operator-supplied allow-list** (new config field enumerating permitted host ports/ranges, checked before binding).
   - Pros: closes the gap completely — only explicitly-approved ports could ever be bound, regardless of what a container's labels claim.
   - Cons: new config schema surface, a design decision about defaults (fail-open vs fail-closed for existing deployments with no allow-list configured), and how it interacts with `internal/core/compose.go`'s auto-generation of this exact label from the operator's own compose file. This is a scope/design decision, not a same-day fix — appropriately handled as its own brainstorm/spec, not bundled into this audit-fix pass.
   - Effort: ~1-2 days including config plumbing and tests.

2. **Reject privileged (<1024) and out-of-range ports in `ParseHostPorts`**, the single function all three label-reading call sites (`manager.go` `ScanAndRegister`, `controller.go`'s two event-handler call sites) already funnel through.
   - Pros: same-day fix, single choke point, no config/schema changes, no behavior change for any legitimate use — `compose.go`'s label generator only ever writes back port pairs already present in the operator's own compose file, which are virtually always application ports >= 1024.
   - Cons: does not fully close the gap — a compromised/malicious container could still claim any unprivileged port (e.g. 8080) that happens to collide with something legitimate, since there's still no cross-check against what the operator actually intended to expose. This is documented as a known partial mitigation, not a full fix.
   - Effort: <1 hour.

3. **Do nothing beyond documenting the risk.**
   - Rejected: the audit's own severity rationale (Medium, not Low) was that this is fail-open trust of attacker-influenceable input; a floor at zero cost to legitimate use is worth taking now.

### Decision
Chose Option 2: added a `minUnprivilegedHostPort = 1024` floor and a 1-65535 range check to `ParseHostPorts` in `internal/proxy/manager.go`. Out-of-range or malformed entries are silently skipped, consistent with the function's existing behavior for malformed pairs (non-numeric, missing colon).

### Rationale
- `ParseHostPorts` is genuinely the single place all three call sites read this label through, so the fix protects `ScanAndRegister` (startup) and both event-driven paths in `controller.go` without needing three separate changes.
- Chose a floor rather than a full allow-list because the full allow-list is a legitimate design question (what's the right default, how does it interact with `compose.go`'s auto-generation) that deserves its own scoped brainstorm rather than being folded into an unrelated audit-fix commit — recording this here so it isn't lost. Suggested follow-up: an opt-in `proxy.allowed_host_ports` config field, cross-checked in `EnsurePort`/`RegisterContainer`, defaulting to "allow all >= 1024" (this fix's current behavior) when unset, so existing deployments are unaffected until an operator opts into stricter enforcement.
- This does not fully close SEC-7 — flagged as "partial mitigation" rather than "fixed" in the audit summary, so a future reader doesn't mistake this for the full allow-list design.

### Verification
- New test cases in `TestParseHostPorts` (`internal/proxy/manager_test.go`): privileged port alone rejected, privileged port dropped from a mixed list while the valid entry is kept, the 1024/1023 boundary, out-of-range host and container ports, and a negative port. All existing `ParseHostPorts` test cases (ports 3306/8080/80, all >= 1024) still pass unchanged, confirming no behavior change for legitimate use.
- `go build ./...`, `go vet ./...`, `gofmt -l`: clean.
- `go test -race ./internal/proxy/... ./internal/watcher/... ./internal/core/...`: all pass, 0 races (`internal/core` included since `compose.go` generates this exact label).

### Rollback Implications
Two files touched (`internal/proxy/manager.go`, `internal/proxy/manager_test.go`), both additive (a new constant + range checks, new test cases). Trivial single-commit revert if ever needed. SEC-7 remains open as a partial mitigation regardless — the full allow-list design (Option 1) is still a valid follow-up.

---

### Decision 18: REL-2 — Lock `entry.mu` Before Reading `LastHealthy` in `GetProvider`

**Date**: 2026-07-29  
**Status**: Implemented

### Context
`docs/audit/2026-07-29-fresh-audit.md` found that `internal/providers/store.go`'s `GetProvider` read `entry.LastHealthy` (and called `entry.Client.Exited()`) without acquiring `entry.mu`, while `MarkProviderHealthy`/`MarkProviderFailure` mutate `LastHealthy`/`ConsecFails` under that same lock — a real data race under concurrent secret fetches.

### Options Considered
1. **Lock `entry.mu` around the `LastHealthy` read in `GetProvider`.**
   - Pros: minimal diff, fixes exactly the race the audit identified, no behavior change to the stale/crash-recovery decision logic itself.
   - Cons: none identified.
   - Effort: <30 minutes.
2. **Switch `entry.mu` from `sync.Mutex` to `sync.RWMutex`** so `GetProvider`'s read could use `RLock` for less contention.
   - Pros: theoretically lower lock contention under heavy concurrent reads.
   - Cons: `GetProvider` also calls `entry.Client.Kill()`/`store.Delete()` in the branches following the read (a broader mutating sequence), and the read itself is a single `time.Time` field copy — negligible contention either way. Switching to `RWMutex` here would be optimizing a lock that's held for nanoseconds, adding a wider API surface for no measurable benefit.
   - Effort: ~30 minutes, no benefit.

### Decision
Chose Option 1: added an `entry.mu.Lock()`/`Unlock()` pair around a single `lastHealthy := entry.LastHealthy` copy at the top of `GetProvider`'s existing-entry branch, then used the local `lastHealthy` variable for the staleness check instead of re-reading the field.

### Rationale
- The race is on a single field read; the minimal fix (copy it out under the existing lock) is correct and leaves the rest of `GetProvider`'s control flow (including the intentionally-unlocked `entry.Client.Exited()` call, which is safe because `plugin.Client` guards its own internal state with its own mutex) untouched.
- No need to introduce `RWMutex` for a lock held for a single field copy — that would add complexity without a measurable win.

### Verification
- New regression test `TestGetProvider_ConcurrentWithMarkProviderHealthy` (`internal/providers/store_test.go`) — runs `GetProvider` and `MarkProviderHealthy` concurrently 50x each against a shared `StoreEntry` (using a zero-value `&plugin.Client{}`, whose `Exited()` is safe to call without a real subprocess since `plugin.Client` guards its own state internally). **Confirmed failing before the fix**: reverting only `store.go` (via `git stash`) and re-running under `-race` reproduces the exact race reported in the audit (write in `MarkProviderHealthy` at `store.go:140` vs. read in `GetProvider` at `store.go:43`). Confirmed passing after the fix, 0 races.
- Full `internal/providers` suite re-run under `-race`: all pass.
- `go build ./...`, `go vet ./...`, `gofmt -l`: clean.
- Full `go test ./...`: all packages pass.

### Rollback Implications
Two files touched (`internal/providers/store.go`, `internal/providers/store_test.go`). Trivial single-commit revert if ever needed — reverting restores the pre-existing (already-tracked) data race.

---

### Decision 19: REL-3/REL-4/REL-5 — Bound Two Unbounded Maps and Fix a Non-Restartable Listener

**Date**: 2026-07-29  
**Status**: Implemented (REL-4's fix revised after an initial version broke mutual exclusion — see below)

### Context
`docs/audit/2026-07-29-fresh-audit.md` found three related Low-severity reliability issues, fixed together as small, independent changes:
- **REL-3**: `internal/events/event_reactor_impl.go`'s `lastSeen` dedup map (keyed by secret name) had no eviction, unlike the sibling `DedupCache`.
- **REL-4**: `internal/rotation/lock_manager.go`'s `LockManager.locks` map entries were never removed after `ReleaseLock`.
- **REL-5**: `internal/events/container_listener.go`'s `Stop()` never reset `cl.ctx`, permanently blocking any later `Start()` on the same instance, plus a redundant blocking `stopChan` send that stalled every `Stop()` call by up to 100ms.

### REL-3: Options Considered
1. **Opportunistic eviction inside `deduplicateSecret`**: sweep entries older than a fixed age (`lastSeenEvictionAge = 1 minute`) every time the function already holds `lastSeenMu` for a write.
   - Pros: no new goroutine, no new lock, piggybacks on a lock already being taken; the sweep only runs when a new secret event arrives, which is exactly when the map is being touched anyway.
   - Cons: sweep cost is O(map size) per call — negligible here since this map is naturally small (distinct secret names), not per-event.
2. **Background cleanup goroutine (like `DedupCache.cleanupLoop`)**: matches the sibling cache's design exactly.
   - Cons: another goroutine + ticker + shutdown path to wire into `EventReactorImpl`'s existing lifecycle, disproportionate for a map whose growth is bounded by distinct secret names, not event volume.
   - **Decision**: chose Option 1 for its far smaller footprint given the actual (mild) severity.

### REL-4: Options Considered
1. **Unconditional `delete(lm.locks, containerID)` in `ReleaseLock`, right after unlocking.**
   - **This was implemented first and found to be unsafe** by the pre-existing `TestLockManager_ConcurrentMutualExclusion` test, which failed after this change (verified: reverting to the ref-counted version below made it pass again in under 0.5s instead of timing out at 10s). The bug: a goroutine that already read the old `*sync.Mutex` out of the map (and is polling `TryLock`) can still lock it *after* a concurrent `ReleaseLock` deletes the map entry, while a third goroutine simultaneously creates a *new* mutex for the same key and acquires that one too — two goroutines then believe they hold "the lock" for the same key at once.
2. **Reference-counted `lockEntry` (`{mu sync.Mutex; count int}`)**: `AcquireLock` increments `count` before touching `mu`; `ReleaseLock` (and the timeout/failure paths in `AcquireLock`) decrement it and only delete the map entry when `count` reaches zero, with an identity check (`cur == entry`) guarding against deleting a different, newer entry that has since replaced this one under the same key.
   - Pros: correct — an entry is only ever removed from the map once no goroutine holds a reference to it, so the "new mutex created while an old reference is still live" scenario above is impossible by construction.
   - Cons: more code than Option 1 (a new type, refcount bookkeeping at every acquire/release/failure path).
   - **Decision**: chose Option 2 after Option 1 was caught failing its own existing test. Mutual exclusion correctness during rotation is a stronger requirement than bounding this map's size, so the more careful fix was the only acceptable option here.

### REL-5: Options Considered
1. **Reset `cl.ctx`/`cl.cancel` and recreate `eventsChan`/`stopChan` in `Stop()`.**
   - Chosen. `Start()`'s "already started" guard checks `cl.ctx != nil`, so it must be cleared for a restart to succeed. `eventsChan` must be recreated because `watchEvents` closes it on exit — a second `Start()` reusing the same (now-closed) channel would mean `Events()` immediately reports closed, and `handleEvent`'s send to it inside the new `watchEvents` goroutine would panic ("send on closed channel").
   - **Also required a follow-up fix**: the initial version of this change reassigned `cl.eventsChan` with no synchronization, which the existing `TestContainerListener_ConcurrentOperations` test (which calls `Events()` concurrently with `Stop()`) caught as a data race under `-race`. Fixed by guarding both the reassignment in `Stop()` and the read in `Events()` with the struct's existing `cl.mu` (already used to guard `lastLabels`). `ctx`/`cancel`/`stopChan` did not need the same treatment — `Stop()` calls `cl.wg.Wait()` before resetting them, which happens-before-orders out any concurrent access from the (by-then-exited) `watchEvents` goroutine, and nothing else reads them.
2. **Leave `Stop()`'s redundant blocking `stopChan` send as-is.**
   - Rejected: since `cl.cancel()` already causes `watchEvents` to exit via its `ctx.Done()` case, the goroutine is essentially always gone by the time `Stop()` attempts the blocking send, so that send times out on its own 100ms window on every call for no benefit. Changed to a non-blocking `select`/`default` — still delivers the signal in the (very unlikely) case `watchEvents` is genuinely waiting on `stopChan` first, but no longer stalls the common case.

### Verification
- **REL-3**: new `TestEventReactorImpl_LastSeenEviction` seeds two stale entries and one fresh one, triggers a sweep via `deduplicateSecret`, and asserts the stale entries are gone while the fresh one and the newly-recorded key remain.
- **REL-4**: new `TestLockManager_ReleaseLock_RemovesMapEntry` confirms `lm.locks` is empty after five sequential acquire/release cycles on different keys. The pre-existing `TestLockManager_ConcurrentMutualExclusion` (20 goroutines contending on one key, run under `-race`) is the test that caught the unsafe first version and now passes in ~0.46s.
- **REL-5**: new `TestContainerListener_RestartAfterStop` confirms `Start()` succeeds again after `Stop()` on the same instance, and that `Events()` returns a fresh, open channel. The pre-existing `TestContainerListener_ConcurrentOperations` (concurrent `Events()` + `Stop()`, run under `-race`) is what caught the eventsChan race and now passes.
- `go build ./...`, `go vet ./...`, `gofmt -l`: clean.
- `go test -race ./internal/events/... ./internal/rotation/...`: all pass, 0 races.
- Full `go test ./...`: all packages pass.

### Rollback Implications
Five files touched: `internal/events/event_reactor_impl.go` + `event_reactor_test.go` (REL-3), `internal/rotation/lock_manager.go` + `lock_manager_test.go` (REL-4), `internal/events/container_listener.go` + `container_listener_test.go` (REL-5). Each of the three fixes is independent and can be reverted separately if needed; REL-4's revert would restore both the unbounded-map issue and, if reverted to the unconditional-delete version rather than the pre-fix baseline, the mutual-exclusion bug — a revert should go back to the original `map[string]*sync.Mutex` design, not the intermediate unsafe version.

---

### Decision 20: QUAL-1/2/3 — Surface Three Previously-Swallowed Errors

**Date**: 2026-07-29  
**Status**: Implemented (QUAL-2 and part of QUAL-3 verified by code review + full suite rather than a forced-failure test — see Verification)

### Context
`docs/audit/2026-07-29-fresh-audit.md` found three places where an error was discarded (`_ = err` or unchecked assignment), each silently masking a real failure from the operator:
- **QUAL-1**: `internal/cli/logs.go`'s `fetchEvents` discarded a JSON unmarshal error, making a malformed agent API response indistinguishable from "no events".
- **QUAL-2**: `internal/core/compose.go`'s `RunComposeUpWithEnv` discarded a `filepath.Abs` error, risking a corrupted `dso.compose.path` label / project name on failure.
- **QUAL-3**: `internal/core/compose.go`'s `PrintRedactedCompose` discarded a `yaml.Marshal` error, silently printing nothing instead of surfacing a compose-parsing problem during `--debug`.

### Decision
For each, checked the error and surfaced it in a way consistent with the function's existing style and signature:
- **QUAL-1**: `fetchEvents` now prints a stderr diagnostic (matching its existing error-reporting style for the "cannot reach agent" case) and still returns `nil`, so callers' behavior is unchanged but the operator now sees *why* no events came back.
- **QUAL-2**: `RunComposeUpWithEnv` (which already returns `error`) now returns a wrapped error instead of proceeding with a zero-value path.
- **QUAL-3**: `PrintRedactedCompose` (a `void` function used only for `--debug` output) now prints a stderr diagnostic and returns early instead of calling `fmt.Println` with an empty/zero-value string.

### Rationale
Each fix matches the shape of its function — return an error where one is already returned (QUAL-2), print a diagnostic where the function's contract is side-effecting output (QUAL-1, QUAL-3) — rather than introducing a new error-return signature that would ripple into unrelated callers.

### Verification
- **QUAL-1**: new `TestFetchEvents_MalformedResponse` (`internal/cli/logs_test.go`) serves malformed JSON from an `httptest.Server`, captures stderr, and confirms both the diagnostic message and the unchanged `nil` return.
- **QUAL-3**: new `TestPrintRedactedCompose_HappyPath` (`internal/core/compose_test.go`) confirms the normal path is unaffected (secret redacted, no stderr output) now that the error is checked. A forced-failure test (feeding a `func()` value into a service map to make `yaml.Marshal` fail) was attempted but discarded: `gopkg.in/yaml.v3` **panics** on unsupported types rather than returning an error, so that specific approach doesn't exercise the new error-return branch at all — it would test a panic path this change doesn't touch. No other practical way to force `yaml.Marshal` to return (rather than panic) was found for this call site's actual data shape (parsed YAML, so only maps/slices/scalars ever appear in practice).
- **QUAL-2**: no dedicated forced-failure test — `filepath.Abs` only fails when `os.Getwd()` fails (e.g. current directory deleted out from under the process), which isn't practically reproducible in a portable unit test without mocking `os` internals. Verified instead by code review (the fix is a straightforward `if err != nil { return ... }` matching the pattern immediately above it in the same function) and the full test suite passing with no regression to `RunComposeUpWithEnv`'s existing (indirect) coverage.
- `go build ./...`, `go vet ./...`, `gofmt -l`: clean.
- Full `go test ./...`: all packages pass.

### Rollback Implications
Four files touched: `internal/cli/logs.go` + `logs_test.go` (QUAL-1), `internal/core/compose.go` + `compose_test.go` (QUAL-2/QUAL-3). All three fixes are small and independent; trivial to revert individually if needed.

---

### Decision 21: SEC-7 (Full Fix) — Operator-Configurable Host-Port Allow-List

**Date**: 2026-07-30  
**Status**: Implemented

### Context
Decision 17 (2026-07-29) applied a partial mitigation for SEC-7: `ParseHostPorts` rejects privileged (<1024) and out-of-range host ports, since `dso.host_ports` is attacker-influenceable (any container can set its own labels). That decision explicitly deferred the full fix — a cross-check against what the operator actually intended to expose — as its own scoped design rather than bundling it into the audit-fix pass. The user has now asked for it.

### Decision
Added an opt-in `proxy.allowed_host_ports` config field (`pkg/config/config.go`'s new `ProxyConfig`), a list of exact ports (`"8080"`) or inclusive ranges (`"3000-4000"`). Parsed into a `*portAllowList` (`internal/proxy/manager.go`) and enforced in `Manager.EnsurePort` — the single choke point all three `dso.host_ports`-reading call sites (`ScanAndRegister`, and `controller.go`'s two event-handler paths) already go through. `internal/cli/agent.go` wires `cfg.Proxy.AllowedHostPorts` into the `Manager` via a new `SetAllowedHostPorts` method, called once at startup before `ScanAndRegister` so the initial container scan is governed by it too.

### Options Considered (recap from Decision 17, now resolved)
1. **Change `NewManager`'s signature** to accept the allow-list at construction.
   - Rejected: ~13 existing test call sites (`internal/proxy/manager_more_test.go`) construct `Manager` via `NewManager(logger)` with no config; changing the signature would force updating all of them for a config value that's genuinely optional and commonly absent (most deployments won't set it).
2. **`SetAllowedHostPorts` method, called post-construction, mirroring the existing `reloader.ProxyManager = proxyManager` post-construction wiring pattern already used in `agent.go`.**
   - Chosen: zero changes to existing `NewManager` call sites; nil allow-list (the zero value) is the default and preserves exactly the Decision 17 behavior (any port >= 1024 allowed) until an operator opts in.

### Rationale
- A nil-safe `allows` method (`func (al *portAllowList) allows(port int) bool { if al == nil { return true } ... }`) means "no allow-list configured" requires no special-casing at call sites — `EnsurePort` always calls `al.allows(hostPort)` whether or not one was ever set.
- Enforcing in `EnsurePort` rather than in each of the three label-reading call sites keeps this fix, like Decision 17's, to a single choke point — a future fourth call site automatically gets the same protection.
- Supporting both exact ports and ranges matches how operators actually describe firewall/allow-list policy (a single service port, or a range for a pool of services) without requiring one entry per port.

### Verification
- New tests in `internal/proxy/manager_more_test.go`: `TestPortAllowList_NilAllowsEverything` (default/zero-value behavior), `TestNewPortAllowList_ExactPorts`, `TestNewPortAllowList_Range`, `TestNewPortAllowList_MixedEntries`, `TestNewPortAllowList_Empty`, `TestNewPortAllowList_InvalidEntries` (malformed port, zero, out-of-range, inverted range, negative), and `TestManager_EnsurePort_RejectsPortOutsideAllowList` (confirms `EnsurePort` rejects a non-allow-listed port, accepts an allow-listed one, and reverts to unrestricted once `SetAllowedHostPorts(nil)` clears it).
- Existing `TestManager_EnsurePort` (which calls `EnsurePort(0, 80)` with no allow-list configured) still passes unchanged, confirming the default path is unaffected.
- `go build ./...`, `go vet ./...`, `gofmt -l`: clean.
- `go test -race ./internal/proxy/... ./pkg/config/... ./internal/cli/...`: all pass, 0 races.
- Full `go test ./...`: all packages pass.

### Rollback Implications
Three files touched: `pkg/config/config.go` (new `ProxyConfig` field, additive/backward-compatible — omitted YAML key means the field is empty, i.e. today's behavior), `internal/proxy/manager.go` (new type + method + `EnsurePort` check), `internal/cli/agent.go` (wiring). All additive; a deployment with no `proxy.allowed_host_ports` in its config is completely unaffected. SEC-7 is now fully closed — no further follow-up tracked.

---

### Decision 22: LINT-1 — Reverting a Behavior Change That Rode Along in the Lint Pass

**Date**: 2026-07-30  
**Status**: Implemented

### Context
An independent code review of the 402-finding lint pass found that one hunk was **not** behavior-preserving, contrary to that commit's stated "no behavior change intended". `internal/cli/doctor.go`'s `padLeft` had an `ineffassign` finding on a `padding` variable that was computed and never used. Instead of deleting the dead computation, the fix started *using* it (`s + strings.Repeat(" ", padding) + " |"`), which changes `docker dso doctor` output. The reviewer further noted the new version doesn't even fix the underlying box misalignment — it just misaligns differently, and emits an ASCII `|` inside a box drawn with `│` (U+2502).

### Decision
Reverted to a true no-op: deleted the dead `padding` computation and restored the original return value (`s + " |"`, byte-identical to the original `s + " " + "|"`). Documented the pre-existing misalignment as a `TODO` on the function rather than silently fixing it, since correcting it means changing `padLeft`'s contract and `printText`'s format strings together.

### Rationale
- A commit that claims "no behavior change" must actually mean it, or the claim stops being load-bearing for reviewers. Fixing the alignment is a legitimate change — it just needs to be its own deliberate commit with test coverage, not a side effect of satisfying a linter.
- Proved equivalence rather than asserting it: ran both the original and replacement implementations over 7 inputs (empty, short, exactly-at-boundary, longer-than-boundary, zero/one length) and confirmed byte-identical output in all cases.

### Verification
- Differential test of old vs. new `padLeft` across 7 inputs: identical in every case.
- `golangci-lint run` (real config): 0 issues — the `ineffassign` finding stays fixed.
- `go build`/`go vet`/`gofmt` clean; `internal/cli` suite passes under `-race`.

### Rollback Implications
One file (`internal/cli/doctor.go`), plus removal of the now-unneeded `strings` import. Trivial revert.

---

### Decision 23: REL-2 (Completion) — A Second, Unlocked Read the Original Fix Missed

**Date**: 2026-07-30  
**Status**: Implemented

### Context
The same code review found that Decision 18's REL-2 fix was **incomplete**. `GetProvider` correctly captured `entry.LastHealthy` under `entry.mu` for the staleness comparison, but three lines later the "Provider connection may be stale, reconnecting" log line re-read `entry.LastHealthy` *unlocked* — the exact race the fix was written to eliminate.

The original REL-2 regression test did not catch this because it seeded `LastHealthy: time.Now()`, so the staleness branch containing the racy log line never executed.

### Decision
Used the already-captured `lastHealthy` local in the log line. Added `TestGetProvider_StaleBranch_ConcurrentWithMarkProviderHealthy`, which seeds a timestamp older than the 10-minute threshold to force the stale branch, and re-seeds the entry inside the loop so later iterations keep reaching it after the branch deletes it.

### Rationale
- The lesson recorded for future audit work: a concurrency fix's regression test must exercise **every branch that touches the shared field**, not just the branch that motivated the fix. Branch coverage, not line coverage, is the relevant standard for race tests.
- `plugin.Client.Kill()` returns early when `runner == nil`, so a zero-value `&plugin.Client{}` makes the stale branch (which calls `Kill()`) safely reachable in a unit test without a real subprocess.

### Verification
- **Confirmed the new test fails against the pre-fix code**: temporarily restoring the unlocked read reproduced `WARNING: DATA RACE` at `store.go:54` and failed the test; restoring the fix made it pass. This is the evidence that the test is not toothless.
- `go test -race ./internal/providers/`: passes, 0 races.

### Rollback Implications
Two files (`internal/providers/store.go`, `internal/providers/store_test.go`). Reverting restores a real data race.

---

### Decision 24: SEC-8 — Provider Plugins and Local Backends Could Inject Empty Secrets

**Date**: 2026-07-30  
**Status**: Implemented

### Context
A fresh audit of previously-unreviewed code (`cmd/plugins/*`, `pkg/api`, `pkg/backend`, `pkg/provider`, `pkg/schema` — never covered by the 2026-07-29 pass) found several ways a *successful-looking* fetch could yield an empty secret, which the agent then treats as success, caches, and injects into containers:

1. `pkg/backend/env`: an unset variable returned `(empty map, nil error)`. A typo'd or unexported variable name was indistinguishable from success.
2. A secret whose body is the literal JSON `null` or `{}` unmarshals **without error** into a nil/empty map. Verified empirically: `json.Unmarshal([]byte("null"), &m)` returns `nil` error and leaves `m == nil`. Affected AWS, Azure, Huawei, and the `file` backend. The `{"value": raw}` fallback only runs on unmarshal *error*, which these inputs don't produce.
3. Worse, in the AWS plugin the resulting nil map then reached `data["_TAG_"+*tag.Key] = *tag.Value`, which **panics** (`assignment to entry in nil map`), killing the plugin process and tearing down the RPC connection. Trigger: any AWS secret whose value is literal `null` that also carries at least one resource tag.

Confirmed the downstream blast radius directly: `internal/agent/server.go` on the `err == nil` path increments `SecretRequestsTotal{"success"}`, writes an audit record with status `success`, and calls `s.Cache.Set(cacheKey, data)` — so a rotation cycle can overwrite a previously-good cached secret with nothing.

### Decision
- `env` backend: switched `os.Getenv` → `os.LookupEnv` and return an actionable error when the variable is **unset**. A variable explicitly set to the empty string is still honored, since that is a deliberate operator choice and `LookupEnv` is what distinguishes the two.
- All four external plugins + `file` backend: after a successful JSON decode, reject `len(data) == 0` with an error naming the secret and suggesting the fix. In the AWS plugin this guard is placed *before* the tag-merge loop, so it also removes the nil-map panic.
- `env`'s `WatchSecret` previously discarded the error (`data, _ := p.GetSecret(name)`) and emitted an update with nil `Data`. It now populates the long-unused `api.SecretUpdate.Error` field instead, so consumers can distinguish a failure from a legitimately empty secret.

### Rationale
- "Fail closed" is the established convention in this codebase for secret material (see SEC-2's mandatory hash verification): silently delivering an empty credential is strictly worse than a loud error, because the failure surfaces later as a confusing application-level auth error instead of at the fetch.
- This also makes the six providers **consistent**: every one now errors on a missing/empty secret. Previously `env` was the sole outlier, which meant callers could not code against a single contract.
- `len(data) == 0` (rather than checking `data == nil` only) covers both the `null` (nil map) and `{}` (non-nil, zero-length) cases with one condition.

### Verification
- Empirically confirmed the root cause before fixing: a scratch program showed `null` → `err=nil, m==nil`, `{}` → `err=nil, len=0`, and that assigning into the resulting nil map panics.
- `go build ./...`, `go vet ./...`, `gofmt`: clean. `golangci-lint`: 0 issues. `gosec`: 0 issues.
- Full `go test ./...`: all 29 packages pass. `-race` clean across `pkg/backend`, `pkg/provider`, `cmd/plugins`.

### Rollback Implications
Five files: `pkg/backend/env/env.go`, `pkg/backend/file/file.go`, and the AWS/Azure/Huawei plugin `main.go`s. **This is an intentional behavior change**: configurations that were silently receiving an empty secret will now fail loudly. That is the point — but it means an operator relying (knowingly or not) on the old empty-secret behavior will see a new error. Reverting restores the silent-empty-secret and AWS panic behaviors.

---

### Decision 25: SEC-9 — Vault Plugin: Mount Escape, Cleartext Token, and Missing Init Guard

**Date**: 2026-07-30  
**Status**: Implemented

### Context
The same fresh audit found four issues in `cmd/plugins/dso-provider-vault`, which had never been reviewed:

1. **Mount escape**: `path := fmt.Sprintf("%s/data/%s", p.mount, cleanName)` interpolated the secret name from `dso.yaml` with no validation. A name like `../../sys/policy/root` or `../../../transit/keys/x` traverses out of the configured KV mount, so a secret entry could read any path the token is authorized for — defeating the implicit expectation that `mount` bounds the provider's reach.
2. **Cleartext token**: `address` defaulted to `http://127.0.0.1:8200` and any configured value was accepted with no scheme check. Since the token is sent as an `X-Vault-Token` header on every request, a plain `http://vault.internal:8200` exposes it to anyone on the network path.
3. **Missing nil-client guard**: every other provider returns a "not initialized" error if `GetSecret` is reached before `Init`; Vault nil-dereferenced `p.client.Logical()` and **panicked the plugin process**. `ProviderRPCServer.GetSecret` exposes this method over RPC with no ordering enforcement.
4. **Non-scalar value mangling**: nested KV values were flattened with `fmt.Sprintf("%v", v)`, turning a JSON object into Go debug syntax (`map[a:1]`) and injecting *that* as the secret value.

### Options Considered (for the cleartext issue)
1. **Require `https` unconditionally.** Rejected: breaks the documented default (`http://127.0.0.1:8200`) and every local-development and Vault-Agent-sidecar setup, where traffic never leaves the host.
2. **Permit `http` only for loopback hosts; require `https` otherwise.** Chosen — it targets the actual risk (token crossing a network) while leaving the legitimate loopback case working, so no existing valid deployment breaks.
3. Warn instead of erroring. Rejected: a warning in a daemon log is not a control, and this codebase's convention for credential exposure is to fail closed.

### Decision
- Added `validateVaultSecretName`, rejecting empty names, absolute paths, and any `..` path segment. Deliberately does **not** reject `/` in general, since nested KV paths (`app/db/password`) are a normal Vault convention.
- Added a defense-in-depth check that `path.Clean(vaultPath)` still carries the `<mount>/data/` prefix, so no traversal form slips past the segment check.
- Added `requireSecureVaultAddr` (Option 2 above), called from `Init`.
- Added the missing nil-client guard.
- Replaced `fmt.Sprintf("%v", v)` with a type switch: strings pass through, nil becomes `""`, and everything else is re-encoded with `json.Marshal`.
- Added an empty-result guard for consistency with Decision 24, and a `--version` flag, which the other three plugins already had and which `docker dso system doctor`/`setup` use for plugin health validation.

### Verification
Six new tests in `cmd/plugins/dso-provider-vault/main_test.go`, all passing:
- `TestValidateVaultSecretName_RejectsMountEscape` — 7 traversal/absolute/empty forms rejected.
- `TestValidateVaultSecretName_AllowsLegitimateNames` — 5 nested paths still accepted, proving the fix targets traversal and not slashes.
- `TestRequireSecureVaultAddr` — 5 permitted (loopback http + https) vs. 5 rejected (remote http, bad schemes), including the built-in default.
- `TestGetSecret_UninitializedClientDoesNotPanic` — asserts an error, with an explicit `recover()` so a regression reports as a panic failure rather than crashing the run.
- `TestInit_RejectsCleartextRemoteAddr` — confirms the guard is reachable from the real entry point, not just in isolation.
- `TestInit_RequiresToken` — confirms the new address validation doesn't mask the pre-existing token requirement.

`golangci-lint` 0 issues, `gosec` 0 issues, `-race` clean.

### Rollback Implications
Two files (`main.go`, new `main_test.go`) in the Vault plugin only; no other provider or the daemon is affected. **Intentional behavior change**: a deployment currently pointing at a remote Vault over cleartext `http` will now fail to initialize. That is the security fix working as designed; the migration is to switch to `https` or a loopback Vault Agent.

---

### Decision 26: SEC-10 — Plugin Env Sanitization Was Silently Breaking All Documented Credential Paths

**Date**: 2026-07-30  
**Status**: Implemented

### Context
`pkg/provider/load.go`'s `sanitizeEnv()` returned exactly one variable — `PATH` — and `cmd.Env = sanitizeEnv()` gave the plugin subprocess nothing else. The intent (don't leak the daemon's environment into a plugin) is sound, but it was over-broad to the point of breaking documented functionality:

- The AWS plugin's own doc comment advertises `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_REGION`.
- The Azure plugin advertises `AZURE_CLIENT_ID`/`AZURE_CLIENT_SECRET`/`AZURE_TENANT_ID`.
- The Huawei plugin calls `os.Getenv("HUAWEI_ACCESS_KEY")`, `HUAWEI_SECRET_KEY`, `HUAWEI_SECURITY_TOKEN`, and `HUAWEI_REGION` **directly** — all of which always returned `""`.
- No `HOME`, so `~/.aws/credentials` and `az login` caches were unreachable.

Its own doc even names `/etc/dso/agent.env` as the `EnvironmentFile` mechanism. So an operator following the documented Huawei IAM-Agency setup would get a credential built from empty strings. Only IMDS-based AWS auth worked, and only by accident. The code and the documentation could not both be right.

### Options Considered
1. **Pass the daemon's full environment through** (`os.Environ()`).
   - Rejected: abandons the security property entirely. The plugin subprocess would see `DSO_MASTER_KEY`, every other provider's credentials, and any unrelated host secret — a real escalation path for a compromised or malicious plugin binary.
2. **Fix the documentation instead**, declaring config-file credentials the only supported path.
   - Rejected: it would mean deleting a genuinely useful and conventional auth mechanism (env/IMDS/CLI-cache chains are the *standard* way cloud SDKs authenticate), and the Huawei plugin's code would still need changing since it reads env directly.
3. **Per-provider allow-list pass-through.** Chosen.

### Decision
`sanitizeEnv` now takes the provider name and passes through only:
- a fixed `PATH` (unchanged — still never inherited),
- a common set (`HOME`, proxy vars, `SSL_CERT_FILE`/`SSL_CERT_DIR`) needed by any provider behind an egress proxy or private CA,
- the credential variables allow-listed for **that specific provider**.

Variables that are allow-listed but unset are omitted entirely rather than passed as empty strings.

### Rationale
- Scoping per-provider rather than using one shared list is deliberate least-privilege: the AWS plugin has no reason to see `AZURE_CLIENT_SECRET`, so a compromised plugin cannot harvest a different backend's credentials. This is strictly stronger than the obvious "one list for everything" fix.
- Omitting unset variables matters more than it looks: passing an empty `AWS_PROFILE` would *shadow* a credential the SDK chain could otherwise have resolved via instance metadata, converting a working setup into a broken one.
- The core security property is preserved and now **tested explicitly** rather than assumed.

### Verification
Seven new tests in `pkg/provider/load_env_test.go`, all passing:
- `TestSanitizeEnv_AlwaysSetsFixedPath` — sets a hostile `PATH` and confirms the fixed default still wins.
- `TestSanitizeEnv_PassesProviderCredentials` — the regression test: 6 credential vars across 4 providers now reach the plugin.
- `TestSanitizeEnv_IsPerProviderLeastPrivilege` — each provider is confirmed **not** to receive the other three's secrets.
- `TestSanitizeEnv_DoesNotLeakUnrelatedDaemonEnv` — `DSO_MASTER_KEY` and other host secrets stay out, for all providers including an unknown one. This is the property the original one-line `sanitizeEnv` provided, now pinned by a test.
- `TestSanitizeEnv_OmitsUnsetVariables`, `TestSanitizeEnv_PassesCommonVars`, `TestSanitizeEnv_UnknownProviderGetsNoCredentials`.

`golangci-lint` 0 issues, `gosec` 0 issues, full suite + `-race` clean.

### Rollback Implications
One production file (`pkg/provider/load.go`) plus a new test file. The change is a **widening** of what plugins can see, so it cannot break a working deployment — it can only make a previously-broken documented path start working. Reverting re-breaks env-var and `HOME`-based credentials for all four providers.

### Known Remaining Gaps (recorded, not fixed here)
The same audit surfaced two issues deliberately left open rather than bundled in:
- **`SecretProviderWithContext` is dead code.** → **Now fixed, see Decision 28.**
- **Provider interface divergence in `WatchSecret` first-delivery.** AWS/Azure/Huawei send an initial value immediately; Vault/`file`/`env` only send on the first tick, so a consumer waiting for an initial value stalls for a full interval on half the providers. Still open.

---

### Decision 27: CLEAN-4 — Removing a Dead `Agent.poller` Field Rather Than Wiring It Up

**Date**: 2026-07-30  
**Status**: Implemented

### Context
Code review flagged that `NewAgent` assigned a `SmartPoller` into an `Agent.poller` field, while `runMainLoop` created a **second, local** `poller := polling.NewSmartPoller()` that shadowed it and was used for all actual polling. The field was therefore written but never read: the constructor's poller, and any adaptive-interval state that would have accumulated through it, was silently discarded.

This is invisible to the `unused` linter, which treats assigning a field as using it — confirmed empirically: with the shadowing bug in place, `golangci-lint` still reported 0 issues.

### Options Considered
1. **Wire it up** — change `runMainLoop` to `poller := a.poller`, making the field meaningful and leaving one instance.
   - Pros: preserves the constructor's evident intent; the Agent "owns" its poller.
   - Cons: **leaves the trap in place.** A future local variable named `poller` would shadow the field again, and — critically — the invariant is *not unit-testable*: `runMainLoop` needs a live Docker daemon and secret configuration to drive, so there is no cheap test that fails if the shadowing returns.
2. **Delete the field** and keep the loop-local poller, which is the only thing that ever used it.
   - Pros: removes the dead allocation *and* the shadowing trap by construction — there is no field left to shadow, so no invariant needs guarding.
   - Cons: the Agent no longer holds a reference to its poller. Nothing needs one (verified: zero readers), so this is YAGNI, not a loss.

### Decision
Chose Option 2. Deleted the `poller` field and its constructor initialization; `runMainLoop` keeps its local instance, now with a comment recording why the field was removed rather than wired.

### Rationale
- Eliminating a class of bug beats testing for it. Option 1 would have required a guard that (as discovered below) could not actually be written.
- **A toothless test was written and then discarded during this work, which is why the decision changed.** The first attempt at Option 1 included `TestAgent_PollerIsSharedNotShadowed`, which asserted that `a.poller` retained state. Verified against the reintroduced bug: **the test still passed**, because it only exercised the field directly and never went through `runMainLoop` — precisely the "warning-only test" anti-pattern that `docs/audit/TEST-1-BACKLOG.md` exists to eliminate. Rather than ship a test that looks like a guard but isn't, the field was removed so no guard is needed.

### Verification
- Confirmed the linter cannot catch the original bug: with the shadowing restored, `golangci-lint run ./internal/agent/` reported 0 issues.
- Confirmed the candidate test was toothless: it passed with the bug reintroduced. Test deleted rather than kept.
- `go build`/`go vet`/`gofmt` clean; `golangci-lint` 0 issues; `internal/agent` suite passes under `-race`.

### Rollback Implications
One production file (`internal/agent/agent.go`): a struct field and one constructor line removed, plus a comment. No behavior change — the polling loop used the local instance both before and after.

---

### Decision 28: REL-6 — Making Context Cancellation Actually Work Across the Plugin RPC Boundary

**Date**: 2026-07-30  
**Status**: Implemented (client-side cancellation; server-side deadline propagation deliberately deferred)

### Context
`pkg/api/plugin.go` declared an optional `SecretProviderWithContext` interface, and `internal/agent/trigger.go` type-asserted for it in two places with comments stating that "agent shutdown cancels in-flight AWS calls immediately." **Nothing implemented it.** `ProviderRPC` exposed only the non-context `GetSecret`, so both assertions always failed and every fetch silently took the blocking path.

Consequence: a provider that stopped responding could block the rotation path indefinitely, and the agent's own root context could not interrupt it. The socket/REST path happened to be protected by an external 30 s `context.WithTimeout` in `internal/agent/server.go`; the trigger/rotation path had no such wrapper. So the documented cancellation guarantee was entirely fictional on the path where it mattered most.

### Options Considered
1. **Implement `GetSecretWithContext` on each of the four plugin binaries.**
   - Rejected as the primary fix: the plugins run in separate processes behind `net/rpc`. Adding the method plugin-side does nothing for the daemon unless the *client* stub (`ProviderRPC`) also exposes it, and the daemon only ever holds `ProviderRPC`. It would also require shipping four rebuilt binaries to fix a daemon-side defect.
2. **Implement it on `ProviderRPC`.** Chosen. `SecretProviderPlugin.Client` returns `&ProviderRPC{...}`, and `LoadProvider`'s `Dispense("provider")` result is exactly that — so one implementation fixes cancellation for **all four external plugins at once**, with no plugin rebuild and no wire-format change.
3. **Change the RPC arguments to carry a deadline** so the plugin bounds its own SDK call server-side.
   - Deferred, not rejected. This is the more complete fix, but it changes the gob-encoded argument type from `string` to a struct, which breaks compatibility with any already-installed plugin binary. Given plugins are SHA256-pinned (SEC-2) and shipped with the release, it is feasible — but it is a wire-protocol change that deserves its own versioned rollout, not a bundle with a daemon-side bug fix.

### Decision
Implemented `ProviderRPC.GetSecretWithContext` using `rpc.Client.Go` (asynchronous dispatch) and a `select` racing `call.Done` against `ctx.Done()`. A context that is already done short-circuits before dispatching, so a shutting-down agent starts no new remote work.

### Rationale and Accepted Trade-off
- `net/rpc` has no cancellation concept, so this **cannot abort the remote work** — it unblocks the *caller*. The abandoned call is reaped by `net/rpc`'s own read loop when the plugin eventually replies, and the plugin process is killed on shutdown regardless. Unblocking the daemon is the actual bug being fixed; the residual is a briefly-orphaned in-flight call, which is strictly better than a wedged rotation loop.
- Deliberately did **not** add `GetSecretWithContext` to the local `file`/`env` backends. Their work is a local `os.ReadFile`/`os.Getenv`, not a network call, and honoring a context there would mean wrapping a non-cancellable syscall in the same goroutine dance for no practical benefit. The assertion in `trigger.go` correctly falls back to `GetSecret` for them.

### Verification
Six new tests in `pkg/provider/provider_context_test.go`, exercising a **real** `net/rpc` client/server pair over `net.Pipe` (not a stand-in), all passing:
- `TestGetSecretWithContext_CancellationUnblocksCaller` — a plugin that blocks forever; the context is cancelled only *after* the plugin is confirmed inside `GetSecret`, so this tests interrupting a genuinely in-flight call. Asserts `context.Canceled` and prompt return.
- `TestGetSecretWithContext_DeadlineUnblocksCaller` — honors a 150 ms deadline against a never-returning plugin (observed: returns in 0.15 s instead of blocking).
- `TestGetSecretWithContext_AlreadyCancelledDoesNotDispatch`, `_HappyPath`, `_PropagatesProviderError` (a real provider error must not be misreported as a context error).
- `TestTriggerStyleAssertionNowSucceeds` — reproduces `trigger.go`'s exact assertion against the value the daemon actually holds, proving the context-aware branch is now selected rather than silently skipped.

Plus **compile-time** assertions (`var _ api.SecretProviderWithContext = (*ProviderRPC)(nil)`). **Confirmed these catch the regression**: deleting the method fails the build with `*ProviderRPC does not implement api.SecretProviderWithContext (missing method GetSecretWithContext)` — a stronger guard than a runtime test, since the defect cannot be reintroduced without breaking compilation.

`golangci-lint` 0 issues, `gosec` 0 issues, full suite (29 packages) passes, `-race` clean.

### Rollback Implications
One production file (`pkg/provider/provider.go`, additive method) plus a new test file. No wire-format or plugin change, so no compatibility risk in either direction. Reverting restores un-cancellable fetches on the rotation path.

### Follow-up Still Open
Server-side deadline propagation (Option 3 above): the plugin currently has no idea the daemon gave up, so its own SDK call continues to completion. Worth doing as a versioned protocol change.

---

### Decision 29: PERF-4/PERF-5 — Removing Two Per-Container API Amplification Patterns

**Date**: 2026-07-30  
**Status**: Implemented

### Context
The 2026-07-28 audit listed five HIGH-severity performance findings (PERF-1..5). Because the code had changed substantially since (v3.5.21's "Smart Polling", plus this month's fixes), each was **re-verified against HEAD before any change** rather than trusted. Two were confirmed as real API-amplification patterns:

- **PERF-5 (N+1 reconciliation)** — `reconcileRuntimeState` issued one `ContainerInspect` per tracked target. It runs on a 10-minute ticker *and* on every Docker daemon reconnect, so 200 tracked containers meant 200 round-trips per cycle, with a burst per reconnect on a flapping daemon.
- **PERF-4 (event filtering)** — confirmed real, but **the audit named the wrong direction**. It said "over-filtering"; the actual problem was *under*-filtering plus ordering. The subscription used bare `type=container`, delivering every container event on the host, and `handleEvent`'s **first** action was a full `ContainerInspect` — the relevance check (`HasRelevantLabels`) ran only afterwards. One `docker exec` on any container produced three events and therefore three inspects; any container with a `HEALTHCHECK` produced them indefinitely, for containers DSO does not manage. Cost scaled with total host activity, not with DSO's tracked set.

### Decision
**PERF-5**: replaced the per-target loop with a single `ContainerList`. Two non-obvious details drove the implementation:
- `All: true` is **required, not incidental**. `ContainerInspect` succeeds for a *stopped* container, so the old code treated "stopped but present" as existing. A default (running-only) list would have reclassified every stopped container as orphaned and silently dropped it from tracking — a correctness regression disguised as an optimization.
- A failed list must **not** be treated as "everything is orphaned". The old shape could not fail this way; the batched shape can. On list error the orphan sweep is skipped for that cycle rather than wiping the entire tracking set on one transient API error.

**PERF-4**: two independent fixes, both applied.
1. Narrowed the subscription to the only actions that can change a container's labels or existence (`create`, `start`, `stop`, `die`, `destroy`, `update` — `update` is how label changes on a running container surface; `destroy` is kept so tracking cleanup still runs).
2. Reordered `handleEvent` to reject irrelevant containers from `event.Actor.Attributes` (the daemon already ships labels in the event payload) *before* inspecting. Guarded on `len(Attributes) > 0`: if an event ever arrived without attributes, an empty map would look like "no relevant labels" and we would silently skip a container we should watch, so that case deliberately falls through to the inspect. Correctness is preferred over the saved call.

### Rationale
- Fix 2 is the more robust half — it holds even for actions that remain subscribed, and even if a future change widens the filter again.
- The `All: true` and list-failure hazards are recorded here because both are cases where the "obvious" batched rewrite is subtly wrong, and a future reader optimizing this code again should not have to rediscover them.

### Verification
- **PERF-5**: 4 new tests in `internal/watcher/controller_perf_test.go` — asserts zero per-container inspects and at most 2 list calls with 3 targets tracked; that genuinely-missing containers are still removed; that a list failure does **not** drop tracking; and that a stopped-but-present container is not orphaned (asserting `?all=1` is actually requested). **Confirmed enforcing**: reintroducing the N+1 loop makes the batching test fail with "reconciliation made 3 per-container inspect calls; expected 0".
- **PERF-4**: 5 new tests in `internal/events/container_listener_perf_test.go` — zero inspects for an irrelevant container, exactly one for a relevant one, the conservative fallback when attributes are absent, cleanup still happening without an API call, and the subscription carrying the action filters while *not* carrying the noisy ones.
- A test-harness bug was found and fixed while writing these: the first version counted the *list* endpoint (`/containers/json`) as an inspect, because it also matches `/containers/` + `/json`. The list case must be matched first. Worth noting since it initially looked like a code failure.
- `golangci-lint` 0 issues, `gosec` 0 issues, build/vet/gofmt clean, full suite passes.

### Rollback Implications
Two production files (`internal/watcher/controller.go`, `internal/events/container_listener.go`) plus two new test files. Behavior-preserving apart from the reduced call volume; the narrowed event subscription means DSO no longer *receives* events it previously received and discarded.

---

### Decision 30: PERF-1/PERF-2/PERF-3 — Deleting an "Adaptive Polling" Loop That Could Never Have Worked

**Date**: 2026-07-30  
**Status**: Implemented

### Context
Verifying PERF-2 against HEAD produced a more fundamental finding than the audit described. `Agent.pollSecret` reads **only DSO's local cache**:

```go
currentVal, ok := a.cache.Get(secretName)   // no provider handle exists here
```

No provider or resolver is reachable from it, and its doc comment ("queries the current version of a secret from its configured backend") was simply false. Consequences:

1. **A provider-side rotation was undetectable by it.** The hash could only change if something *else* had already written the new value locally, making the "detector" a downstream observer of the change it was supposed to discover.
2. **Every secret fired one spurious rotation event at startup** (`!exists` on the first poll returns `changed=true`).
3. `Agent.rotationCallback` — the sink for both the polling path *and* the container-listener path — is an **admitted stub**. Its own comment read "In a production implementation with TriggerEngine, this would call ExecuteRotation… For now, record that rotation was triggered and log." It rotates nothing.

Meanwhile `TriggerEngine.StartPolling` (`internal/agent/trigger.go`) does the real work: `Store.GetProvider` → `GetSecretWithContext`/`GetSecret` → hash → `ExecuteRotation`. That is what `internal/cli/agent.go` runs for the `dso agent` daemon. So the `agent.go` loop was a **non-functional parallel duplicate**.

Separately, PERF-3 split in two, as suspected: goroutine *accumulation* was genuinely fixed in v3.5.21 (stop channels + `BUG-1`'s mutexes), but *churn* remained and was worse than "per interval change" — `updateTicker` was called **unconditionally on every poll**, closing a channel, allocating a channel, stopping a ticker, creating a ticker and spawning a goroutine even when the interval was identical (the common case, since `GetNextInterval` returns one of three constants). At the 5s tier that is 12 full teardown/spawn cycles per minute per secret.

A **shutdown panic** was also found in the same code: `defer close(tickersChan)` was registered *after* the ticker-stop defer, and defers run LIFO — so the channel closed while goroutines were still parked in `select { case tickersChan <- name: ; case <-ctx.Done(): }`. With both cases ready Go selects uniformly at random, giving each parked goroutine roughly a coin-flip chance of a send-on-closed-channel panic.

### Options Considered
1. **Wire a provider handle into `agent.go`'s poller** so it makes real calls.
   - Rejected: two independent pollers would then hit provider APIs for the same secrets — duplicated cost and duplicated rotation triggers — and the sink is still a stub, so it would rotate nothing anyway.
2. **Leave the code, correct only the docs.**
   - Rejected: smallest diff, but leaves dead machinery, spurious startup rotations, per-poll churn and a probabilistic shutdown panic in place.
3. **Delete the polling machinery; keep the container-listener/reactor path.** Chosen (and confirmed with the user before deleting, since it removes a documented headline feature).

### Decision
Removed `pollSecret`, `getSecretsToMonitor`, `startPollingGoroutines`, `updateTicker`, `cleanupStaleSecrets`, `GetSecretVersionsMapSize`, `GetLastCleanupTime`, the `secretVersions`/`tickers`/`tickerStopChans` maps and their mutexes, and `tickersChan` (which removes the panic by construction). Kept the `ContainerListener` → `EventReactor` path, which PERF-4 had just improved. Simplified `rotationCallback` and replaced its misleading comment with an explicit **KNOWN GAP** note stating plainly that it performs no rotation and naming where real rotation lives.

Deleted the tests that exercised only the removed machinery, including all of `agent_ticker_race_test.go` (it tested the race on the now-deleted `tickers`/`tickerStopChans` maps).

### Rationale
- Deleting beats wiring: the loop's sink was a stub, so wiring providers into it would have added real API cost for still-zero functional benefit.
- Kept the honest framing rather than quietly improving it — the new `rotationCallback` comment says it does not rotate, instead of the old comment's implication that rotation happened elsewhere in the queue processor (which was not true).
- **Consequence stated plainly rather than hidden:** `polling.SmartPoller` now has **zero production callers**. It is retained (implemented, unit-tested) because adopting it inside `TriggerEngine.StartPolling` — where provider calls genuinely occur — is the change that would make the advertised feature real. `unused` will not flag it, since exported symbols in a package with no importers are not reported.

### PERF-1 (documentation)
The "not wired" half of PERF-1 was already closed by Decision 27 (`CLEAN-4`). The "false advertising" half was **still present, for a larger reason than the audit gave**: the docs quantified provider-API savings from a loop that made zero provider API calls, so both the numerator and denominator of every figure were zero. Corrected:
- `docs/SMART_POLLING.md` — added a prominent implementation-status note; relabeled the 28,800→3,260 arithmetic as an unmeasured projection; marked `dso_polling_api_calls_saved_total` as never implemented (confirmed: no polling metrics exist in `pkg/observability`); removed the "80% reduction" and "80–95% combined" claims.
- `docs/EVENT_DRIVEN_ROTATION.md` — status note on Tier 1; annotated "One HTTP API call per poll" and the projected savings table.
- `README.md` — replaced both 80% claims with the actual status.
- `CHANGELOG.md` `[3.5.21]` — **annotated, not rewritten.** A released entry is a historical record; the correction is added inline as a pointer and stated fully under `[Unreleased]`.

### Verification
- `TestAgent_RunMainLoop_RepeatedShutdownDoesNotPanic` runs 25 start/cancel cycles with cancellation racing startup, since the original defect was probabilistic and a single pass could pass by luck.
- `internal/agent` suite runtime dropped from ~10s to ~1.4s, consistent with the spurious polling work being gone.
- `go build`/`go vet`/`gofmt` clean, `golangci-lint` 0 issues, `gosec` 0 issues, full suite passes.

### Rollback Implications
Touches `internal/agent/agent.go` (large deletion), `internal/agent/agent_test.go`, deletes `internal/agent/agent_ticker_race_test.go`, and edits four docs. **No functional loss**: the deleted loop detected nothing and its sink rotated nothing. Reverting restores the dead machinery, the spurious startup rotations, the per-poll churn and the shutdown panic.

---

### Decision 31: Closing the Remaining Backlog — Provider Consistency, TEST-1, SEC-1.1, Deadline Propagation

**Date**: 2026-07-31  
**Status**: Implemented

This entry covers the five items that were still open after Decisions 22–30.

---

#### (a) `WatchSecret` first-delivery divergence

AWS/Azure/Huawei emitted an initial value immediately; Vault, `file` and `env` only sent on their first tick, so a consumer waiting for an initial value stalled a full interval on half the providers. Applied the existing AWS `send()` + immediate-first shape to the other three. All six now behave identically.

Verified by `pkg/backend/watch_firstdelivery_test.go`, which uses a **one-hour** watch interval and a 3-second deadline: only an immediate first send can satisfy it, so the test cannot pass by accident. Also covers the error case (an unset env var must report promptly rather than after an interval) and confirms a pre-cancelled context still closes the channel instead of emitting.

---

#### (b) AWS `_TAG_` key shadowing

`GetSecret` merged AWS resource tags into the secret map as `_TAG_<key>` with a plain assignment, so a tag could silently overwrite secret material. This matters because tags are typically writable by a **broader IAM population** than `secretsmanager:GetSecretValue` readers — someone able to set a tag named `password` (→ `_TAG_password`), or literally `_TAG_password`, could shadow a real key.

Secret data now always wins and the collision is reported on stderr. Extracted the loop into `mergeTags` so the precedence rule is unit-testable without an AWS client; tests cover shadowing, non-colliding tags, nil `Key`/`Value` pointers (which would otherwise panic the plugin), and empty/nil tag lists.

---

#### (c) TEST-1 — toothless tests

A full repo sweep covered **127 test files**. 13 genuinely toothless instances were confirmed by reading code; 5 would have failed if naively converted.

**The most important result: none of the 5 was a production defect.** Each was a wrong test expectation or a broken test fixture — the opposite of TEST-1's origin story (SEC-4, where the toothless test was hiding a real vulnerability). Recorded because it changes how the next sweep should be approached: assume the test is wrong before assuming the code is.

Per-instance triage:
- **`TestIsSafePathEmptyPaths`** (the known instance) — verified independently with a scratch program that `IsSafePath("", "")` returns `(".", nil)`. The `baseDir == ""` branch is a deliberate "anywhere mode" (rejects absolute paths outside the allow-list and `..` traversal, then returns the cleaned path). The test's `wantErr: true` was simply wrong. **Production code unchanged**, expectations corrected, and the assertion strengthened to also check the returned path.
- **`TestEventReactorImpl_QueueMaxBatch`** — reported "expected 12, got 10" forever. A test arithmetic bug, not a defect: `batchTimeout=5s` with `dequeueBatch(5)` means 12 events need **three** ticks (~15s), but it slept 12s. Replaced the fixed sleep with polling; now passes in 15.03s, exactly as predicted.
- **`TestStress_EventDebouncer_RapidFire`** — the fixture was broken: `i % int(10000*0.8)` is `i % 8000`, true for exactly `i=0` and `i=8000`, so only **two** events ever reused an ID. The debouncer was never exercised (1 duplicate detected against 7200 expected). Fixed the generator to match its stated 80% intent; now detects **7992** duplicates, and the check is a real assertion.
- **`TestSmartPolling_APICallReduction`** — called *no production code*; pure arithmetic over hardcoded intervals, with a guard (`reduction < 0`) that fired every run. Rewritten to exercise `polling.CalculateInterval` and assert the honest relationship in **both** directions. This produced a genuinely useful result (see below).
- **`rotationCalls` strict-equality checks** — equality was the wrong contract (the reactor deduplicates), so these became meaningful lower bounds with the reasoning stated inline, rather than being forced into assertions that would be flaky.

Four further instances were converted mechanically after confirming they pass. Timing/resource heuristics (goroutine-count and memory-growth warnings in the stability suites) were deliberately **left as logs** — converting them buys flaky CI, and that judgement is recorded rather than silently applied.

**Bonus finding, corroborating PERF-1:** the rewritten polling test shows the adaptive schedule uses **more** calls than fixed 30s polling over a 15-minute window (41 vs 30), and **88.7% fewer** over 24 hours (326 vs 2880). So the docs' 88.7% figure was arithmetically correct for a day-long window — the problem corrected in Decision 30 was that those polls never happen at all, not that the arithmetic was wrong. The docs' current wording ("unmeasured projections of the tiering arithmetic") is accurate on both counts.

---

#### (d) SEC-1.1 — redaction performance

`RedactString` ran all 11 patterns through `ReplaceAllString` unconditionally. That call allocates a fresh result string **even when the pattern does not match**, which is the common case — so a typical log field paid 11 scans and 11 allocations to change nothing.

**Options considered:**
1. **Combined single-alternation regex** (backlog option 1) — rejected. Merging 11 patterns changes match/overlap semantics and application order, which is exactly the "faster path that redacts *less*" failure the backlog explicitly warned about. Not worth that risk inside a security control.
2. **Cheap keyword pre-scan** (backlog option 2) — rejected for the reason the backlog itself gives: it must stay manually in sync with the patterns or it becomes a silent bypass.
3. **Guard each `ReplaceAllString` with a non-allocating `MatchString`.** Chosen. This is **provably** equivalence-preserving rather than heuristic: when `MatchString` is false, `ReplaceAllString` is defined to return the input unchanged, so skipping it cannot alter the result.
4. **Avoid the `redactFields` slice allocation** (backlog option 3) — **declined.** Detecting "nothing changed" requires comparing `zapcore.Field` values that contain an `interface{}`, which is not reliably comparable. Trading that risk inside a security control for one allocation out of four is a bad deal; recorded here so the decision is deliberate rather than an oversight.

**Measured (same harness the backlog specified):**

| Benchmark | Before | After | Change |
|---|---|---|---|
| `Redaction_StringField` | 24,274 ns/op, 1,783 B/op, **70 allocs** | ~13,600 ns/op, 531 B/op, **4 allocs** | ~44% faster, **94% fewer allocations** |
| `Redaction_ReflectField` | 47,840 ns/op, 2,967 B/op, **90 allocs** | ~28,400 ns/op, 1,358 B/op, **24 allocs** | ~41% faster, **73% fewer allocations** |

**Correctness evidence** — the acceptance criterion was "proves the optimization doesn't change *what* gets redacted, only *how fast*". Speed numbers alone do not establish that, so two differential tests were added that run the guarded implementation against a retained copy of the original unguarded one and require **byte-identical** output: a hand-picked matrix (no-match, single-pattern, multi-pattern ordering, and adversarial inputs like `"[REDACTED]"`, `"password="`, `"://:@"`), plus a fuzzy pass over **2,197** three-fragment combinations. All 37 subtests named in the SEC-1.1 acceptance criteria pass unchanged.

---

#### (e) Server-side deadline propagation (completes Decision 28)

Decision 28 deferred this because widening `GetSecret`'s gob-encoded argument from `string` to a struct would break every already-installed plugin binary.

**Resolved additively instead:** a new `Plugin.GetSecretWithDeadline` RPC method carrying `GetSecretArgs{Name, Deadline}`. An older plugin answers with net/rpc's "can't find method" error, which the client detects and transparently falls back from — so no plugin rebuild is required and no wire format changed. The deadline is sent as an **absolute time**, not a duration, so the plugin isn't working from a timer that started when the message was sent.

To make the deadline meaningful rather than decorative, all four plugins now implement `api.SecretProviderWithContext`, each refactored to an internal `getSecret(ctx, name)`:
- **AWS / Azure / Vault** — genuinely honor it; their SDKs accept a context (Vault via `ReadWithContext`/`ReadWithDataWithContext`).
- **Huawei** — **documented limitation**: `ShowSecretVersion` takes no context, so an in-flight call cannot be interrupted. It performs a pre-flight `ctx.Err()` check so an already-expired deadline starts no CSMS call at all. Daemon-side cancellation still unblocks the caller regardless. Real interruption needs SDK support.

Tests prove the deadline actually crosses the process boundary (server-side records what it received and compares against the caller's), that a caller **without** a deadline does not get one fabricated, that an older plugin exposing only the legacy method still works, and that `isUnknownRPCMethod` does not misclassify a genuine provider error as "method missing" (which would silently retry on the legacy path).

---

### Verification (all five)
`go build`/`go vet`/`gofmt` clean; `golangci-lint` 0 issues; `gosec` 0 issues; full `go test ./...` passes.

### Remaining Open
- Huawei in-flight interruption (blocked on SDK context support).
- `redactFields` slice allocation (declined above, with reasoning).
- Coverage-padding tests noted by the sweep (`TestFormatDuration`, `TestIsTerminal`, `store_more_test.go`) — a different anti-pattern from TEST-1's warning-only tests; worth a sibling backlog item rather than folding in here.

---

## How to Use This Log

### When Reviewing Code
Reader asks: "Why was this designed this way?"  
→ Check DECISION_LOG.md for context and trade-offs

### When Proposing Changes
Maintainer thinks: "Should we change this?"  
→ Check DECISION_LOG.md to see what was deliberately chosen vs accidental

### During Retrospectives
Team asks: "What did we learn?"  
→ Review DECISION_LOG.md to identify patterns (e.g., "security decisions took longer than expected")

---

## Guidelines

### When to Record a Decision
- **Major architectural choice** (merge vs archive, new subsystem)
- **Security trade-off** (fail open vs fail closed)
- **API/compatibility decision** (breaking change or backward-compatible workaround)
- **Performance optimization** (trading memory for speed, etc.)
- **Deferred work** (deliberately deferring a fix with justification)

### What NOT to Record
- Trivial bug fixes ("added null check")
- Routine refactoring ("split function into two")
- Minor code cleanup

### Format
- Keep it concise (2-3 paragraphs per decision)
- Include date and decision maker (for context traceability)
- Always include "why this wasn't the alternative"
- Be explicit about trade-offs accepted

---

## After All Phases

Review this log to understand:
- What design choices shaped the implementation
- What constraints existed at decision time
- What became easier/harder as a result
- What would be different with different choices

This becomes the institutional memory of the audit-driven refactor.
