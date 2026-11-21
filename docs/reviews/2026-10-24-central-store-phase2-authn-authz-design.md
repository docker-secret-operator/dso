
# `dso central` Phase 2 Step 5.2 — Authentication & Authorization Design (design only, no implementation)

**Date:** 2026-10-24
**Status:** Design document only. No Go source, tests, API, schema, migration, configuration default, or dependency was modified to produce this document. No socket, listener, credential file, or enforcement code was created.
**Builds on:** `docs/reviews/2026-10-24-central-store-phase2-threat-model.md` (Step 5.1, including its §10a review clarifications) and `docs/central-store-m1.md`. The P-01 policy (`docs/reviews/2026-10-24-p01-config-permissions-decision.md`) is **accepted** and binding on any new credential mechanism proposed here.

---

## 1. Purpose and scope

Define — on paper only — what "authenticated" and "authorized" should mean for `dso central`, which callers exist, what each should be permitted to do, and which of the architecture alternatives from Step 5.1 (§9) actually delivers that, with its real limitations stated rather than assumed away. This document does not implement a socket, a credential, or an authorization check. It is the input to a future, separately-approved implementation phase.

## 2. Verified current implementation (not assumed)

- `internal/cli/central.go` (562 lines) registers exactly these operations, confirmed directly from source (`grep` on `Use:` strings, lines as cited): `central init` (line 164), `central project create`/`list` (lines 302, 319), `central environment create`/`list` (lines 346, 363), `central secret create`/`add-version` (lines 558-559, both via a shared `valueCmd` helper), `central secret get` (line 446), `central secret list` (line 473), `central secret versions` (line 498), `central secret disable-version` (line 523), `central secret delete` (line 543). This matches `docs/central-store-m1.md:195-199`'s command summary exactly — no discrepancy found.
- `internal/centralsvc` (package files: `service.go`, `binding.go`, plus `*_test.go`) is the only caller of `internal/store` + `internal/crypto.KeyProvider`; `docs/central-store-m1.md:23` states it is "the only place plaintext exists" — this document treats that as the authorization boundary's natural location (any future auth check belongs at `Service`'s method boundary, not duplicated in the CLI layer), consistent with Step 5.1 §4's point that the check must sit *above* the existing OS-level file-access boundary, not inside the CLI binary alone.
- **No socket, listener, or credential file exists for `dso central` today.** Confirmed by the absence of any `net.Listen`/socket-path constant in `internal/centralsvc` or `internal/cli/central.go` (the only I/O is direct SQLite file access via `internal/store/sqlite`, per `docs/central-store-m1.md:21-23`).
- **A real precedent for peer-credential-based local IPC authorization already exists elsewhere in this codebase** — `internal/agent/server.go` (lines 326, 351, 482, 496) plus `internal/agent/peercred.go`, `peercred_linux.go`, `peercred_other.go`. This is the agent's own local control/status socket (unrelated to `dso central`), but it is the concrete mechanism this document evaluates for reuse. Verified from source, not assumed:
  - `peerAuthorized(p peerIdentity, selfUID int, dsoGID int) bool` (peercred.go:59-82) grants access if the peer's UID is `0` (root), **or equals the agent's own UID**, or the peer's primary/supplementary group is the `dso` group. It explicitly does **not** distinguish between two different processes that both run as the same UID — the doc comment states this plainly: "the check... grants no privilege beyond 'root or dso group' — the same set the socket's 0660 root:dso filesystem permissions intend, validated at the protocol layer" (peercred.go:35-39).
  - `readPeerIdentity` uses `unix.GetsockoptUcred(..., unix.SO_PEERCRED)` and is **Linux-only** (`peercred_linux.go:12-13`); `peercred_other.go:7-11` makes it a no-op elsewhere, falling back to filesystem-permission gating only, per `peercred.go:9-13`'s own comment.
  - `os/user` with `CGO_ENABLED=0` (DSO's release default) reads `/etc/passwd`/`/etc/group` directly, **not** NSS sources like LDAP/SSSD — a user defined only there fails to resolve and is denied, which the code comment calls a fail-closed outcome (peercred.go:40-43). This document treats that as a correct, acceptable property (fail-closed on NSS-only identity), not a defect.

This confirms Step 5.1's own caution (§10a clarification #1) with concrete evidence: the one peer-credential mechanism DSO already has **does not and cannot** distinguish between two processes sharing the same UID — it was deliberately designed as a *coarser* boundary ("root or this UID or this group"), not a *finer* one.

## 3. Design goals and non-goals

**Goals:**
- Add an authorization layer for `centralsvc` operations that is at least as strong as, and does not regress, the existing OS-level file-permission boundary.
- Narrow (not eliminate) the gap identified in Step 5.1 §3/§4: multiple processes sharing the legitimate operator's OS identity should, where technically possible, be distinguishable by *intent* (did this request come through the sanctioned entry point) even if not by *compromise-resistance* (a truly malicious process running as that UID can still impersonate the sanctioned entry point unless a stronger boundary — Option C below — is adopted).
- Produce a design that a future implementation phase can build against without re-deriving the threat model.

**Non-goals (explicitly excluded, consistent with Step 5.1 and ROADMAP.md's Group D exclusions):**
- Multi-tenant RBAC, SSO, or a hosted control plane.
- Remote/multi-host access (Step 5.1 §10a clarification #2 confirms same-host automation is the Phase 2 target; this document does not revisit that).
- A network listener of any kind.
- Solving same-UID compromise completely — this document states explicitly, per the task's own instruction, that **no Unix-socket-based design can fully close this gap without a privileged service, a separate OS identity, or an additional credential mechanism** (§5 below evaluates whether any of those are worth adopting for Phase 2; the recommendation is "not yet," with the limitation disclosed rather than hidden).

## 4. Caller identities and trust boundaries

| # | Caller | In scope for Phase 2? | Identity DSO can actually establish | Operations that could be permitted | Threats remaining outside the model |
|---|---|---|---|---|---|
| 1 | Interactive `dso central` CLI, invoked by the operator | **Yes — primary target** | The invoking process's OS UID/GID (via direct filesystem access today; via `SO_PEERCRED` if Option B is adopted) | All currently-defined operations (§7) | A compromised terminal/shell session under the operator's own account — indistinguishable from the operator by any OS-identity-based mechanism |
| 2 | Local CI/CD process or deployment script, same OS user | **Yes — secondary target, same-host only** (Step 5.1 §10a clarification #2) | Identical to #1 under any OS-identity mechanism — DSO has no way to distinguish "the CLI" from "a script running as the same user" beyond what the OS already enforces | Narrower than #1 in principle (e.g., read-only secret retrieval for a deploy step), but **DSO cannot currently enforce that narrowing** without an application-level credential carrying its own scope (Option D, §5) — under Option B alone, #1 and #2 are the same caller class | Same as #1; additionally, a compromised CI script has the same access as the operator unless scoped by Option D |
| 3 | Process running under a *different* OS user | **Out of scope to permit; in scope to correctly deny** | OS UID, reliably | None by default | Already handled by the existing file-permission/`securefs` boundary (Step 5.1 §3) — this is the one boundary that already works correctly today and must not be weakened by a new mechanism |
| 4 | `root`, or the owning OS account if compromised | **Explicitly out of scope to defend against** (carried forward from Step 5.1 §8 and M1's own stated limits, `docs/central-store-m1.md:58-61`) | Full — root/the owning account can always do everything | N/A — this is a correctly disclosed limit, not a design target | Everything; this document does not propose changing that disclosure |
| 5 | `dso agent` (no central-store call path today) | **Out of scope for this document** — flagged in Step 5.1 §10a item 3 as a future integration decision, not resolved here | N/A — no code path exists | N/A | If and when the agent is wired to `centralsvc`, it becomes caller #1/#2's peer and needs its own authorization entry, decided at that time |
| 6 | Any future remote caller | **Explicitly out of scope** (Step 5.1 §9 Option C rejected for Phase 2; §10a clarification #2) | N/A | N/A | Not analyzed here — would need its own threat model if ever proposed |

## 5. Architecture alternatives and comparison

| | **A. CLI-only, OS-level access control** | **B. Unix-domain socket + peer credentials** | **C. Separate OS service identity / privileged service** | **D. Application-level credential (file-based token)** |
|---|---|---|---|---|
| **Security property actually achieved** | Cross-UID isolation only (already works, via `internal/securefs`) | Same as A, plus a single shared entry point for multiple local callers to go through one code path instead of each needing direct file access — **does not add same-UID attacker resistance** | Can achieve true same-UID attacker resistance **if** the service runs as its own dedicated OS account and callers authenticate to it by a mechanism stronger than "same UID" (e.g., it reads the credential in D) | Adds a distinguishable "is this the sanctioned caller" signal *within* the same UID, **if** the token file itself is properly protected (fail-closed per P-01) — but a malicious process with read access to that token file (i.e., running as the same UID, with the same filesystem access) can still read and use it |
| **Same-UID attacker resistance** | None (not claimed) | **None** (explicitly disclosed — peercred.go's own design accepts this; verified in §2) | Yes, if combined with D or an equivalent credential the privileged service alone can verify | Partial — raises the bar from "run any code as this UID" to "read this specific file as this UID," which a fully compromised account defeats anyway, but which *does* defend against, e.g., an unrelated unprivileged script or dependency running under the same account without having been granted that specific credential |
| **Privilege/operational complexity** | None (current state) | Low — reuses an existing, tested pattern (`internal/agent/peercred.go`) | High — a new privilege-separated service is a significant new operational surface (its own startup/supervision, its own failure mode, its own attack surface) for a product whose stated identity is a simple, self-hosted, single-binary install | Low-to-moderate — one new file, with generation/rotation/revocation semantics to design (§8) |
| **Credential storage/recovery** | N/A | N/A (no credential; socket file permissions only) | A credential is still needed somewhere unless the privileged service does its own OS-level peer-cred check on *its own* process boundary, which only moves the same-UID question to "who can reach the privileged service," not eliminate it | New fail-closed credential file; needs rotation/revocation design (§8) |
| **Portability/platform constraints** | None | **Linux-only for the peer-credential part** (confirmed §2); falls back to filesystem-permission-only on other platforms, same as today | Platform-dependent service-management tooling (systemd, etc.) — adds more platform-specific surface than B | Fully portable (plain file read) |
| **Compatibility with current architecture** | Full (no change) | High — additive, doesn't replace the existing CLI-direct-file-access path, callers can still use either | Low — would likely require `centralsvc` to stop being directly invoked by the CLI process at all, a larger architectural change than Phase 2's stated scope | High — additive on top of either A or B |
| **Testing/maintenance burden** | None (current state) | Low — `internal/agent/peercred_test.go` already demonstrates the testing pattern for this exact mechanism | High — new service lifecycle, new failure modes to test | Low-to-moderate — credential generation/rotation/expiry logic needs tests |
| **New trust boundary introduced?** | No | A *shared entry point*, not a new trust boundary (still "OS UID" underneath) | **Yes** — a genuinely new boundary (a distinct OS identity) | A *partial* new boundary (possession of a specific file, within the same OS UID) |

### Recommendation (not a unilateral decision — for product-owner approval)

**Adopt Option B now, as a shared local entry point, with Option D's credential-scoping idea deferred rather than built in Phase 2.** Rationale: Option B is the smallest design that achieves the one concrete, near-term goal Phase 2 actually has (giving the CLI and a same-host automation script one consistent, testable entry point to `centralsvc`, instead of each needing raw file access) — it does **not** claim to solve same-UID compromise, and this document states that limitation explicitly rather than overstating it, per the review's clarification #1. Option C is rejected for Phase 2 as disproportionate operational complexity for a product whose stated identity is a simple self-hosted install; Option D's scoping benefit (narrowing caller #2's permitted operations below caller #1's) is real and worth a future design once Option B's basic plumbing exists, but is not required to make initial progress and would otherwise be designed speculatively against an authorization matrix (§7) that doesn't exist in code yet.

## 6. Recommended authentication model (proposed design, not implemented)

- **"Authentication" in this model means**: establishing the connecting process's OS UID/GID via `SO_PEERCRED` on Linux (reusing the exact, already-tested `internal/agent/peercred.go` pattern), falling back to filesystem-permission-only gating on other platforms — identical fallback behavior to the existing agent IPC socket, for consistency and because it is already a proven, documented tradeoff rather than a new one.
- **"Authorization" in this model means**: a policy function analogous to `peerAuthorized`, evaluated against the *operation being requested* (§7's matrix), not just "connect or don't" — this is the one concrete difference from the existing agent-socket precedent, which only gates connection, not per-operation permissions, because the agent socket's own operation set is narrower. `dso central`'s richer operation set (secret read vs. write vs. delete) justifies a per-operation check, not just a per-connection one.
- **Authentication failure vs. authorization denial, explicitly distinguished**: a connection from a peer whose identity cannot be established at all (e.g., `SO_PEERCRED` unsupported/failed, or NSS-only identity that doesn't resolve per §2's note) is an **authentication failure** — reject before any operation is considered, fail closed, log only the raw UID/PID (never derive or guess a username). A connection from a peer whose identity *is* established but who is not permitted the *specific* requested operation (e.g., caller #2 attempting a delete that policy reserves for #1) is an **authorization denial** — reject with a distinct error class, so operators can tell "I'm not who you think I am" apart from "you know who I am but can't do that," without revealing anything about *other* callers' permissions in the denial message.
- **No application-level credential is proposed for this iteration**, consistent with the Option B recommendation above — this section intentionally leaves "how would caller #2 be granted a narrower scope than caller #1" as an explicit unresolved decision (§12), not a speculative mechanism designed without a concrete need yet.

## 7. Proposed authorization matrix

Grounded in the exact operations confirmed in §2 from `internal/cli/central.go`. Columns distinguish **Verified current operation** (exists in code today) from **Proposed** (not yet implemented, named here only to pre-register where Step 5.3/5.4 will need their own authorization entries).

| Operation | Status | Proposed authorization requirement | Enforceable in current CLI-only model? | Requires Option B (or stronger)? |
|---|---|---|---|---|
| `central init` (create key + database) | Verified (central.go:164) | Caller #1 only — this is a one-time, high-privilege bootstrap action; should require the same identity class that will own the resulting files | Yes, today — it's gated by filesystem creation rights, which is already the right boundary for an operation that *creates* the trust boundary itself | No — this one is arguably **better left CLI-only even after B exists**, since it predates having any `Service` to authorize against |
| `central project create` / `environment create` | Verified (central.go:302, 346) | Callers #1 and #2 (write access) | Partially — today, anyone with file access can do this; no finer distinction exists | Yes, if caller #2 is ever to be distinguished from #1 |
| `central project list` / `environment list` | Verified (central.go:319, 363) | Callers #1 and #2 (read access) | Same as above | Same as above |
| `central secret create` / `add-version` | Verified (central.go:558-559) | Callers #1 and #2 (write access) — the operation that actually introduces new secret material | Same as above | Same as above |
| `central secret get --reveal` (plaintext read) | Verified (central.go:446) | Should be the **most restrictively scoped** operation in any future per-operation policy — this is the one place M1 prints plaintext (`docs/central-store-m1.md:206`) | Today: identical access as everything else (whoever has file access can do this) — this document flags that as the single highest-value target for Option D's finer scoping (§5), **not implemented here** | Strongly — this is the operation most worth distinguishing caller #2 from caller #1 for, if that distinction is ever built |
| `central secret list` / `versions` (metadata only, no values) | Verified (central.go:473, 498) | Callers #1 and #2 (read access) | Same as general read case | Same as above |
| `central secret disable-version` / `delete` (soft-delete) | Verified (central.go:523, 543) | Caller #1 only, recommended — these are destructive-adjacent and arguably should be scoped more tightly than plain reads/creates, pending product-owner input | Today: no distinction exists | Yes |
| Key/credential management (root-key rotation, DEK rotation) | **Not implemented** (`docs/central-store-m1.md:142`: "Not in M1") | Not applicable yet — no operation exists to authorize | N/A | N/A — defer until the operation itself is designed |
| Backup/restore (Step 5.4) | **Proposed, not implemented** | Caller #1 only, recommended — a destructive operation (restore) on the entire store | N/A today | Yes, when designed |
| Audit-history access (Step 5.3) | **Proposed, not implemented** | Read access for #1; write/append is internal to `centralsvc` itself, not a caller-invoked operation | N/A today | Depends on Step 5.3's own design |

**`created_by`/`disabled_by` are not used anywhere in this matrix as an identity or authorization source** — per Step 5.1's own carried-forward finding and the task's explicit instruction, these remain caller-supplied attribution strings only (`docs/central-store-m1.md:225`), not a trust input.

## 8. Credential lifecycle and fail-closed requirements

No application-level credential is proposed for implementation in this iteration (§6), so this section is written as a **constraint on any future proposal**, per the P-01 policy and the task's explicit requirements, rather than a specification of a credential this document introduces:

- **Any future `dso central` credential file must default to fail-closed permission enforcement** — reject on open if group/world-readable, wrong owner, or reached through a path with an unsafe symlink/directory-chain, mirroring `internal/securefs`'s existing convention for key files (verified pattern in the earlier security audit's F-06 finding), **not** `pkg/config/permissions.go`'s advisory-only `dso.yaml` convention. This is the P-01 policy's binding constraint, restated here as a design requirement rather than optional guidance.
- **Never store the raw credential value in logs, audit events, command output, or error messages** — an authentication failure should log only the non-secret identity material already available (UID/PID from `SO_PEERCRED`, or "credential file unreadable/malformed" without echoing its contents), matching the existing pattern already verified in `internal/audit/audit.go` and `internal/injector/docker.go` (secret *names*/identifiers only, never values — confirmed in the earlier complete-product security audit's F-05).
- **Minimize lifetime and scope**: if a credential is ever introduced (e.g., to support Option D's caller-#2 scoping), it should be scoped to the narrowest operation set that caller actually needs (per §7's matrix) rather than granting full parity with caller #1 by default.
- **Generation**: should happen through an explicit `dso central` subcommand (not auto-generated silently on first use), so the operator can see and control when a new credential is minted.
- **Rotation/revocation**: a credential scheme without a revocation path is a liability — any future design must define how an issued credential is invalidated before its natural expiry (if any), not just how it's created.
- **Accidental disclosure**: the same class of risk `dso.yaml` already carries (P-01) — a leaked credential file grants whatever scope it was issued, which is exactly why minimizing scope (above) matters.
- **Backup/recovery**: if a credential file is itself encrypted or derived from a passphrase, its loss/recovery story should be decided consistently with the root-key loss story already documented (`docs/central-store-m1.md:144-145`: unrecoverable by design, back up separately) — not given a weaker "we can always reset it" assumption that then undermines the stated security property.
- **Fail-closed behavior for missing/malformed/expired/unreadable credentials**: in all four cases, the correct behavior is to treat the request as unauthenticated (§6's distinction) and deny, never to fall back to a permissive default — this generalizes the same principle already applied in `internal/auth.Verify`'s *bind-time* enforcement (REST API refuses to start open on a public bind, per the earlier security audit's F-02) to this hypothetical future mechanism.

## 9. Threat and failure analysis

| Threat | Mitigated / Detected / Documented-and-accepted | Rationale |
|---|---|---|
| Unauthorized local connection (different UID) | **Mitigated** | Socket filesystem permissions (`0660 root:dso`-style, per the existing agent-socket precedent) plus `SO_PEERCRED` verification both independently block this; this is the one case the existing mechanism (§2) is actually designed for and does correctly |
| Same-UID malicious process | **Documented-and-accepted limitation, not mitigated** | Explicitly disclosed in §2/§5 — no Unix-socket-based design closes this without Option C or D; this document does not claim otherwise |
| Different-UID local process | **Mitigated** | Same as "unauthorized local connection" above |
| Socket path replacement or symlink attacks | **Mitigated, if the implementation reuses `internal/securefs`'s existing directory-chain/symlink conventions for the socket path's parent directory** — not yet implemented, but the mechanism to reuse already exists and is already audited (F-06) | This document recommends any future implementation apply the same `rejectEscapingSymlink`-equivalent discipline to the socket path as already applies to key/database files, rather than inventing a new check |
| Stale or overly permissive socket files | **Mitigated by design convention, not yet implemented** — the agent's existing socket precedent uses exclusive creation and fixed permissions; a future `dso central` socket should follow the same pattern | Not a new design question; reuse the existing pattern |
| Credential theft/revocation/rotation | **Not applicable in this iteration** (no credential proposed) — see §8 for the constraints any future credential must satisfy | — |
| Concurrent requests and authorization-state changes | **Documented, not fully resolved here** — `internal/centralsvc`'s existing SQLite-transaction-level concurrency guarantees (confirmed in the M1 doc: "SQLite's locking serialises writers... the next version number is computed inside the write transaction," `docs/central-store-m1.md:187-190`) already handle data-level races; an authorization check added at the `Service` method boundary should be evaluated per-request against current policy, not cached across a connection's lifetime, to avoid a stale-authorization window — this is a requirement for the future implementation, not something this design resolves itself | — |
| Service startup failure or unavailable socket | **Should fail closed, not fall back to raw file access silently** — a future implementation must decide whether the CLI's direct-file-access path (Option A) remains available as a fallback when the socket is down, or whether that fallback itself constitutes bypassing any authorization the socket was meant to add. **This document does not resolve that tension** — it is flagged as an unresolved decision (§12) because allowing silent fallback to Option A would make Option B's authorization meaningless whenever the socket happens to be unavailable | — |
| Platform differences in peer-credential APIs | **Documented, not mitigated** | Non-Linux platforms get filesystem-permission-only gating, identical to the existing agent-socket precedent's own disclosed limitation (§2) — this document does not propose closing that gap, since the existing precedent already made and documented the same tradeoff |
| Accidental exposure through logs/diagnostics | **Mitigated by design requirement** (§8) | No raw credential or secret value should ever reach a log; this mirrors existing, already-audited conventions (F-05) |
| Compromised `dso agent`, if later granted access | **Out of scope for this document** | Flagged in §4 row 5 and Step 5.1 §10a item 3 as a future integration decision; this document does not analyze it further since no such access exists today |
| Root compromise and rollback limitations | **Documented-and-accepted, carried forward unchanged** | Per Step 5.1 §8 and M1's own disclosure — this document does not attempt to close either gap, consistent with the instruction not to overstate what any proposed design achieves |

## 10. Platform and operational considerations

- **Linux is the only platform with full peer-credential support**, matching DSO's own stated primary support target (`docs/central-store-m1.md:32`: "Supported and tested: Linux"). Any future implementation inherits the same macOS/FreeBSD "compiles but untested" and Windows "fails closed via `internal/securefs`" posture already documented for the rest of the central-store subsystem — this document does not propose a different platform support stance for the socket than M1 already has for the database/key files.
- **Operational complexity**: Option B adds one new long-running listener to supervise (start/stop lifecycle, socket file cleanup on restart) — this is a real, if modest, new operational surface versus today's zero-listener CLI-only model, and should be weighed against the concrete benefit (§5) before implementation.
- **Testing burden**: `internal/agent/peercred_test.go` already demonstrates the testing pattern (synthetic UIDs/GIDs against `peerAuthorized` directly, without needing a real multi-user environment) that a `dso central` equivalent could reuse — this lowers the expected testing burden relative to inventing a new approach.

## 11. Residual risks and explicit non-goals

- **Same-UID compromise is not solved by this design**, stated here one more time plainly: adopting Option B changes *nothing* about what a malicious process running as the operator's UID can do, versus today's direct-file-access model. The benefit is a single, auditable, testable entry point — not a stronger security boundary against that specific threat.
- **Non-goals, restated**: no RBAC, no SSO, no hosted control plane, no remote access, no change to `dso.yaml`'s existing enforcement, no new credential mechanism implemented in this iteration (only its future constraints are specified, in §8).
- **The backup/restore (§5.4) and audit-chain (§5.3) authorization entries in §7 are placeholders**, not designs — they exist only so this matrix doesn't silently omit operations those future steps will need to slot into the same authorization model this document proposes.

## 12. Unresolved decisions requiring product-owner approval

1. **Approve or amend Option B as the Phase 2 architecture**, with its explicitly-disclosed limitation (does not resist same-UID compromise) accepted as-is, rather than deferred for a stronger mechanism (Option C/D) now.
2. **Decide whether caller #2 (local automation) should ever be scoped more narrowly than caller #1 (interactive CLI)** — if yes, Option D's credential-scoping idea (§5, §8) becomes a near-term follow-on design, not a "someday" item; if no, Option B alone may be sufficient for the foreseeable Phase 2 scope.
3. **Decide the fallback behavior when the socket is unavailable** (§9, "Service startup failure or unavailable socket") — does the CLI retain a direct-file-access path as a fallback (potentially bypassing any per-operation authorization the socket adds), or does the CLI *require* the socket once it exists? This materially affects how much security benefit Option B actually delivers in practice.
4. **Decide the authorization granularity for `central secret get --reveal`** (§7) — is per-caller-class sufficient, or does the plaintext-reveal path warrant its own, stricter policy (e.g., requiring caller #1 specifically, never caller #2) regardless of the general read/write split used elsewhere?
5. **Confirm the destructive-operation scoping proposed in §7** (`disable-version`/`delete` restricted to caller #1) — this is this document's recommendation, not a decision already made.

## 13. Acceptance criteria for a future implementation phase

(Stated for the benefit of whichever phase eventually implements this — not something this document itself satisfies.) An implementation should be considered complete only when:
- The chosen architecture (§12 item 1) has been explicitly approved, including its disclosed residual risk.
- Every operation in §7's matrix has a concrete, tested authorization check, with the same-UID limitation (§11) documented in user-facing security documentation, not just this internal design doc.
- Authentication failures and authorization denials (§6) are distinguishable in logs/error output without leaking information about other callers' permissions.
- Any credential introduced satisfies every requirement in §8, with regression tests for fail-closed behavior on missing/malformed/unreadable credential files.
- The socket-unavailable fallback decision (§12 item 3) has been made and is covered by a test exercising that exact condition.

## 14. Evidence index

- `internal/cli/central.go:45,164,299-320,343-364,405-473,498,523,543,558-559` — verified current command set.
- `internal/centralsvc/` (file listing: `service.go`, `binding.go`, `aad_helper_test.go`, `binding_test.go`, `service_test.go`) — verified package composition; `docs/central-store-m1.md:23` for its documented role.
- `internal/agent/peercred.go:1-82`, `peercred_linux.go:12-33`, `peercred_other.go:7-11`, `internal/agent/server.go:26-27,81,326,351,482,496` — verified existing peer-credential precedent, read and cited directly (not assumed from the filename alone).
- `docs/central-store-m1.md:5-7,21-23,32,48-65,100,142,144-145,187-190,195-210,224-227,233` — verified M1 state and limitations, cited per-claim throughout this document.
- `docs/reviews/2026-10-24-central-store-phase2-threat-model.md` §§3,4,8,9,10,10a — Step 5.1 baseline, carried forward and cited per-section above.
- `docs/reviews/2026-10-24-p01-config-permissions-decision.md` — accepted policy, applied as a binding constraint in §8.
- `docs/reviews/2026-10-24-dso-complete-product-audit.md` (security section, findings F-02, F-05, F-06) — cited for existing, already-verified conventions this design reuses (fail-closed bind-time checks, name-only logging, `securefs` symlink/directory-chain discipline).

## 15. Addendum — §12 decisions recorded (approved 2026-10-24)

The five unresolved decisions in §12 have been reviewed and approved. This addendum records them; **no code, API, schema, credential, or socket was implemented to produce it.**

| # | §12 item | Decision |
|---|---|---|
| 1 | Architecture | **Approved**: Option B (local Unix-domain socket + peer credentials via `SO_PEERCRED`), with the same-UID limitation (§11) explicitly documented and not claimed to be solved. |
| 2 | Local automation scope | **No narrower scope for now.** Caller #2 (local automation) is not distinguished from caller #1 (interactive CLI) under Option B alone. Option D (application-level credential) is deferred — to be revisited only if/when automation needs isolation from interactive-CLI access. |
| 3 | `central secret get --reveal` | **Policy intention, not an enforceable control under Option B**: the intended policy is to restrict this operation to caller #1. Peer credentials (`SO_PEERCRED`) identify the connecting process's OS UID/GID only — they cannot distinguish two different processes sharing that same UID. A future implementation **must not claim this restriction is enforced** while Option B is the only mechanism in place; it must be documented as an unenforced policy intention until a stronger caller-identity mechanism exists (§12 item 1's "future requirement," below). |
| 4 | `disable-version` / `delete` | **Same caveat as #3**: intended policy is caller #1 only; not enforceable between same-UID processes under Option B alone. Document as a policy intention, not an active control, until a stronger identity mechanism exists. |
| 5 | Socket unavailable | **Approved**: once socket-based service mode is configured, the CLI must **require** the socket and **must not** silently fall back to direct SQLite file access. On socket unavailability, fail closed with a clear error and recovery guidance (e.g., instructing the operator to restart the service or explicitly fall back to a documented, deliberate CLI-only mode — not an automatic, silent one). This is the decision flagged as most important: a silent fallback would make Option B's authorization boundary meaningless whenever the socket happens to be down. |

### Interim security posture (binding on any future implementation)

- **Document, plainly and in user-facing security documentation (not only this internal design doc), exactly which operations are available to *all* authorized local callers sharing the service's OS UID** under Option B as implemented — i.e., state the real, currently-enforceable boundary ("any process running as this UID or in the `dso` group can perform every operation in §7's matrix") rather than presenting the caller #1-only restrictions on `get --reveal`/`disable-version`/`delete` as if they are active authorization controls when they are not yet enforceable.
- **Do not present aspirational restrictions as active authorization controls** anywhere this design is described to users — this applies to future release notes, `docs/central-store-m1.md`-equivalent documentation for whatever milestone implements this, and any `--help` text, not just this document.

### Future requirement (not resolved by this addendum)

Enforcing the caller-#1-only policy intentions in #3/#4 requires a stronger identity mechanism than OS-UID-based peer credentials alone — specifically one of:
- A **distinct OS identity** for the interactive-CLI caller versus the automation caller (effectively Option C's separate-identity idea, scoped narrowly to just this distinction rather than a full privileged service), or
- A **separately authenticated application-level identity** (Option D's deferred credential, §5/§8), scoped specifically to carry a "this is caller #1" claim rather than merely "this is caller #2 with a narrower operation set."

Neither is approved or designed here — this addendum only records that closing #3/#4's gap requires one of them, consistent with §11's residual-risk disclosure, and flags it as the natural next design question if/when caller-specific enforcement becomes a real requirement rather than a stated intention.

---

**No implementation, staging, commit, or push occurred to produce this document or this addendum.** Only this one file (first created, then amended by this addendum) exists as a result of Step 5.2. See the close-out confirmation below.
