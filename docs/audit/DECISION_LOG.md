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
- No second review round triggered: this single round found no new production-impacting defects requiring further investigation (unlike each of SEC-1's three rounds)

### Rollback Implications
`git revert`; validation = confirm `DSO_PLUGIN_HASH_MANIFEST` becomes optional again (unset env var + no manifest = successful load, matching pre-SEC-2 behavior).

---

### Decision 4: SEC-3 — Plugin Directory Permissions in Container

**Date**: [TBD]  
**Status**: [Proposed]

### Context
Plugin directory is currently writable by daemon user. Options:

1. **Root-owned, 0555** — Read-only for daemon user
2. **Root-owned, 0750** — Writable only by root, readable by daemon
3. **Mount read-only** — Docker volume mount as read-only
4. **No change** — Accept current risk

### Decision
**Chosen**: Root-owned, 0555 (read-only)

**Rationale**: Simplest, no runtime mount complexity, prevents post-compromise persistence.

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
