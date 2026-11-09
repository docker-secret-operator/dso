# `dso central` Phase 2 Threat Model (Step 5.1 — design only, no implementation)

**Date:** 2026-10-24
**Status:** Design document only. No API, schema, authentication, authorization, audit-chain, credential-storage, or backup/restore code was changed to produce this document, per the approved Phase 2 scope boundary.
**Builds on:** the existing M1 threat model already documented in `docs/central-store-m1.md` ("Threat model," "Known limitations") and the P-01 decision record (`docs/reviews/2026-10-24-p01-config-permissions-decision.md`), now **accepted**: new central-store credential files default to fail-closed permission enforcement; `dso.yaml` itself is unchanged.

---

## 1. Current state (verified from code and docs, not assumed)

`dso central` (M1) today:
- Is a **local CLI only** — `internal/cli/central.go`, backed by `internal/centralsvc.Service` (composes `internal/store` + `internal/crypto.KeyProvider`), storing data in a local SQLite file via `internal/store/sqlite`.
- Has **no network listener, no authentication, no authorization** (docs/central-store-m1.md:5-6, 202-210, 224-225, verified unchanged by this session's own read of `internal/centralsvc/service.go` file listing).
- Access control today is **entirely** "whoever has OS-level file access to the database/key files, under the directory-chain checks `internal/securefs` enforces" (docs/central-store-m1.md:224).
- Is **not wired into the REST API or WebUI** (docs/central-store-m1.md:208) — `internal/server`'s HTTP surface (session auth, CSRF-via-origin, rate limiting — all audited in the complete-product audit) does not yet apply to central-store data at all.
- Audit trail: `created_by`/`disabled_by` are **caller-supplied attribution strings**, explicitly documented as "not an audit log and not tamper-evident" (docs/central-store-m1.md:225) — this is exactly the gap Phase 2 Step 5.3 will need to close, but is out of scope for this document.

This document's job is to decide the trust model *before* any of Phase 2's later steps (5.2 auth, 5.3 audit chain, 5.4 backup, 5.5 projects/environments) design against it.

---

## 2. Intended users and deployment topology

DSO's product identity (per the complete-product audit, §9, and ROADMAP.md) is a **self-hosted, single-operator, Docker Compose-native** secrets operator — not a multi-tenant SaaS platform. `dso central`'s intended user population inherits that:

- **Primary user: the single operator** who already runs `dso agent`/`dso up` on a host and wants durable, versioned, encrypted secret storage with a real schema (projects/environments) instead of the flat per-agent vault model. This is the same person who already has root or docker-group access to the host.
- **Secondary, narrower user: an automation identity** (a CI/CD pipeline, a deployment script) that needs to read or write secrets non-interactively on the operator's behalf. This is the main place where "is it strictly local-only" actually matters (§9 below) — a CI runner is very often *not* the same host as the one running `dso agent`.
- **Explicitly not a target user for Phase 2**: multiple independent human operators with different privilege levels on the same store (that is RBAC/multi-tenancy, explicitly out of scope per ROADMAP.md:191 and the original audit's Group D exclusions), and not a hosted/shared control plane serving multiple customers.

**Deployment topology today**: one host, one SQLite file, one (or occasionally a small number of) OS-level operator account(s) with filesystem access. Phase 2 must decide whether that topology is the *permanent* design constraint or a *current* one that a narrowly-scoped local Unix-socket API could relax without becoming "network-accessible" in the sense that matters for this threat model.

---

## 3. Trusted and untrusted local processes

| Process | Trust level | Rationale |
|---|---|---|
| `dso central` CLI invoked by the OS user owning the key/database files | **Trusted** | This is the only "caller" the current design recognizes at all — it inherits the invoking user's OS-level file access, same as any other local tool operating on files it owns |
| `dso agent` (the rotation daemon) | **Trusted, but currently has no code path to `centralsvc` at all** | Central store is not wired into the agent/rotation/injection path yet (docs/central-store-m1.md:208) — this is a Phase 2+ integration question, not resolved by this document |
| Any other process running as the same OS user (intentionally or via a compromised dependency) | **Untrusted, but currently indistinguishable from the legitimate CLI** | This is the core gap Step 5.2 (authentication/authorization) must close — M1's security boundary is "the OS user," not "the `dso central` binary specifically," so a malicious process running as that user has identical access today |
| A process running as a *different* OS user, even on the same host | **Untrusted, currently blocked by `internal/securefs`'s file/directory permission checks** | Verified in the complete-product audit's security review (F-06) and re-confirmed in this review of docs/central-store-m1.md:66-89 — this is the one access-control boundary that already exists and works today |
| `root` or the account `dso central`'s files are owned by, if compromised | **Untrusted and explicitly unprotected against** | Already disclosed in docs/central-store-m1.md:58-61 ("Does not protect against: compromise of the OS account that runs DSO... `root`") — Phase 2 should not attempt to change this; it is a correct, honest scope boundary, not a gap to close |

---

## 4. Threats from a compromised local user

If the OS account `dso central` runs as (or a process impersonating it) is compromised:
- **Everything that account's files already protect is lost** — the attacker has the same access the legitimate operator has: they can read, create, and list secrets, and (today) there is no audit trail distinguishing their actions from the legitimate operator's (docs/central-store-m1.md:225, re-confirmed above).
- **What is NOT lost**: the attacker cannot forge a valid ciphertext or binding canary without the root key (docs/central-store-m1.md:161, "an attacker with raw write access can drop triggers but still cannot forge a valid ciphertext or canary without the keys") — so a compromised account that has file access but *not* the unwrapped root key (e.g., it's a `passphrase`-provider deployment and the attacker doesn't have the passphrase) is still blocked from reading secret values, even though it can corrupt or delete rows.
- **Phase 2 implication**: authentication/authorization (5.2) should be scoped to answer "is this the legitimate operator/automation identity acting through `dso central`'s intended interface," not "is this the right OS user" (which is already enforced) — i.e., it adds a layer *above* the existing OS-level boundary, for the case where multiple processes share that OS account (the actual gap identified in §3 above), not a replacement for it.

## 5. Threats from stolen credentials

Two distinct "credentials" exist in this system and must not be conflated:
1. **The root key / passphrase** (`internal/crypto.KeyProvider`) — theft of this, combined with the database, is already explicitly out of scope for M1's protection (docs/central-store-m1.md:60, "theft of database and root key together"). Phase 2 should not attempt to change this either; it's a correct boundary given no HSM/KMS integration exists yet (docs/central-store-m1.md:100, "Neither is HSM-grade").
2. **A future authentication credential for `dso central`'s own access control** (Step 5.2, not yet designed) — this is new, and its theft model needs its own analysis once 5.2 actually proposes a mechanism. This document only constrains what that mechanism may look like: per the accepted P-01 policy, **any new credential file 5.2 introduces must default to fail-closed permission enforcement**, not the advisory-only model `dso.yaml` uses.

## 6. Threats from filesystem access and database modification

Already covered by M1's existing threat model (docs/central-store-m1.md:48-65) and re-verified unchanged in this review:
- Database-file-alone theft: protected (ciphertext + wrapped keys only).
- Row-level tampering/swapping between projects/environments/secrets/versions: protected (AAD binding, fails closed).
- A different root key silently taking custody of a database: protected (key binding/canary).
- Silent corruption: protected (GCM tag).
- **Whole-database rollback to an older valid copy**: explicitly **not detected** (docs/central-store-m1.md:61, 227) — this is directly relevant to Step 5.4 (backup/restore) and Step 5.3 (audit chain), both still to come; this document flags it as a known, carried-forward gap rather than resolving it here.

## 7. Recovery requirements and backup exposure

- Backups (the `.pre-migration-v<N>...bak` snapshots) are created with the same protections as the live database (`0600`, exclusive, same directory-safety checks) and are **never automatically deleted** — they are a long-lived, growing attack surface with the same sensitivity as the live database (docs/central-store-m1.md:184-186).
- **No backup/restore *command* exists yet** (docs/central-store-m1.md:186, 233) — Step 5.4 will need to design one. This document's constraint for that design: a backup file is exactly as sensitive as the live database and must get the *same* fail-closed permission/directory treatment, not a lesser one, and should not introduce a plaintext export path by default (already a stated requirement in the Phase 2 brief's §5.4).
- **Root-key loss is unrecoverable by design** ("Losing the root key makes all data unrecoverable; back it up separately from the database," docs/central-store-m1.md:144-145) — this is a correct, disclosed tradeoff of the current key-hierarchy design (no escrow/HSM), not something this threat model proposes changing.

## 8. What this design does not protect against (explicit, not silently assumed)

Carried forward from M1, re-verified, and still true for any Phase 2 work that keeps the current single-host/SQLite/local-filesystem architecture:
- Compromise of the OS account `dso central`/`dso agent` runs as.
- `root` on the host.
- Theft of the database **and** the root key/passphrase together.
- An attacker able to replace the database file with an older, validly-signed copy (rollback).
- Anyone who can already run `dso central` as the owning OS user (today, that's the entire access-control model — Step 5.2 narrows this, it doesn't eliminate the underlying OS-trust assumption, since DSO does not run as a privilege-separated system service with its own identity distinct from "the operator's OS account").
- Metadata confidentiality: secret **names**, project/environment slugs, timestamps, and actor strings are stored in clear and are not protected by the encryption scheme (docs/central-store-m1.md:64) — this remains true regardless of the local-only vs. network-accessible decision below, and should be stated plainly to any user evaluating this for a use case where secret *names* themselves are sensitive.

---

## 9. Local-only vs. network-accessible: explicit analysis (not a silent assumption)

This is the question this document was specifically required to analyze rather than assume. Three options, with consequences for each:

### Option A — Remain strictly local-only (CLI invoked directly, no listener of any kind)
- **Consequences**: simplest to reason about; the trust boundary stays exactly "OS-level file access," which is already implemented and verified. No new network attack surface. **Cost**: the "secondary user" from §2 (a CI/CD pipeline or remote automation identity) cannot use central-store secrets without either (a) running on the same host as the database, which defeats much of the point of CI/CD, or (b) some out-of-band mechanism (e.g., `ssh` + local CLI invocation) that this design does nothing to secure or simplify — i.e., staying local-only does not actually prevent remote use, it just means DSO provides no first-class support for it and the operator bears the full risk of whatever ad hoc remote-access mechanism they build themselves.
- **Fit with DSO's identity**: strongest fit — "no mandatory vendor control plane, self-hosted" is DSO's core differentiator (complete-product audit §9), and a local-only design can never accidentally become a hosted control plane.

### Option B — Local Unix-domain socket, same-host only, still no network listener
- **Consequences**: allows multiple local processes (the CLI, a future `dso agent` integration, a local automation script) to talk to `centralsvc` through a defined protocol instead of each linking the Go package directly or shelling out to the CLI — this is what actually closes the §3/§4 gap ("any process running as the same OS user is currently indistinguishable from the legitimate CLI") IF the socket itself enforces a narrower authorization check than "any process owned by this OS user" (e.g., peer-credential checking via `SO_PEERCRED`/`getsockopt`, already a pattern DSO uses elsewhere — `internal/agent/peercred.go` was found during the complete-product audit). Still has zero network exposure; a Unix socket is filesystem-permission-gated by definition, inheriting the same `internal/securefs` directory-safety conventions DSO already uses for the key/database files.
- **Fit with DSO's identity**: still strong — no network surface, no "mandatory control plane," but better supports the realistic CI/CD use case (a CI runner that is a container *on the same host* as the agent, which is DSO's actual common case per its Docker Compose-native identity) without taking on remote-network risk.
- **Recommendation signal**: this option is the one that most directly answers the §2 "secondary user" gap without crossing into network-accessible risk, and is consistent with patterns DSO already has precedent for (peer-credential checking).

### Option C — Network-accessible (TCP/HTTP, even if bound to loopback by default)
- **Consequences**: this is a materially different risk class, even "loopback by default," because (a) it reintroduces every concern the complete-product audit already raised about `internal/server`'s REST API (auth, CSRF, rate limiting, origin checks) — all of which would need to be re-derived or reused for central-store data specifically, doubling the authentication/authorization surface this project must maintain; (b) "loopback by default" is a default, not a guarantee — operators reconfigure defaults, and the existing REST API's own fail-closed bind-check (`internal/server/rest.go:1311-1327`, audited earlier) exists precisely because that exact mistake is foreseeable; (c) it is the option most likely to drift toward "DSO needs a control plane" territory the product has explicitly committed not to build (ROADMAP.md, complete-product audit's Group D exclusions).
- **Fit with DSO's identity**: weakest fit of the three, **not because network access is inherently wrong, but because it is the highest-risk, highest-maintenance-burden way to solve the §2 problem when Option B solves the same real use case (same-host automation) with substantially less exposure.**
- **This document does not recommend Option C for Phase 2.** If a genuine multi-host use case later justifies it, it should be a separate, explicitly-scoped decision with its own threat model — not something Phase 2's auth/audit/backup work should design around by default.

### Recommendation (for review, not a unilateral decision)
**Option B (local Unix-domain socket, peer-credential-gated, same-host only)** is this document's recommendation for Phase 2's actual scope — it resolves the real gap (§3/§4: distinguishing the legitimate caller from "any process owned by this OS user") without taking on network-accessible risk, and is consistent with DSO's existing architectural patterns and stated self-hosted identity. **`dso central` should remain local-only in the network sense (no TCP/HTTP listener) for Phase 2.** This is a recommendation for your approval, not a decision made unilaterally by this document, consistent with the instruction that this must be explicitly analyzed rather than assumed.

---

## 10. Unresolved security decisions and assumptions requiring review before Step 5.2

1. **Confirm or reject Option B** (§9) as the scope boundary for Step 5.2's authentication/authorization design, versus Option A (stay CLI-only, no new protocol at all) if the "secondary user" automation case isn't actually a near-term priority.
2. **Peer-credential-gating mechanism**: if Option B is confirmed, Step 5.2 will need to decide exactly what "authorized" means over that socket — e.g., matching the connecting process's UID against the database owner, versus a finer-grained scheme. This document does not resolve that; it only establishes that *some* mechanism tighter than "any process owned by this OS user" is the actual security gain Phase 2 should aim for.
3. **Whether `dso agent` itself should ever call into `centralsvc`** (today it does not at all) — if Phase 2 wires the agent to read rotation secrets from the central store, the agent process becomes a new trusted local caller, and its own compromise model (already covered in the complete-product audit) becomes relevant to central-store data for the first time. This document flags that integration as a future decision point, not something resolved here.
4. **Metadata confidentiality** (§8): confirm whether "secret names and project/environment slugs are stored in clear" remains an acceptable, disclosed limitation for Phase 2's target users, or whether it should be revisited — this document carries the existing M1 position forward without change, since changing it would be a cryptographic-design decision well beyond Step 5.1's scope.
5. **Audit-chain interaction with the unresolved rollback gap** (§6/§8): Step 5.3's audit-chain design will need to explicitly decide whether it also closes (or explicitly continues to accept) the "whole-database rollback to an older valid copy is not detected" gap, since an attacker who can roll back the database can also roll back its audit trail along with it. Flagged here so 5.3 doesn't design the audit chain in isolation from this still-open gap.

---

## 10a. Review clarifications (recorded 2026-10-24, incorporated for Step 5.2 to honor)

Three clarifications from review, to be treated as binding constraints on Step 5.2's design rather than optional notes:

1. **A Unix socket does not by itself distinguish the legitimate CLI from another process sharing the same OS identity.** Peer-credential checking (`SO_PEERCRED`) establishes the connecting process's *OS user*, not "is this specifically `dso central`'s intended caller" — two processes running as the same OS user remain indistinguishable to it. Step 5.2 must state precisely what security property the socket adds (narrowing from "any process with filesystem access to the key/DB files" to "any process running as this OS user, at the moment it connects") and must not describe this as resolving the full §3/§4 gap — only as narrowing it to the same granularity the existing file-permission boundary already has, for the different code paths (RPC-shaped calls vs. direct file access) that can now share one authorization check instead of two.
2. **Same-host automation is the Phase 2 target, not remote CI/CD access.** A local Unix socket does not solve a remote CI runner's access problem, and this document does not claim it does. Confirmed as a scope decision: Phase 2 targets same-host automation only; remote/multi-host access is out of scope unless separately approved with its own threat model.
3. **Rollback-detection is an explicit, unresolved Step 5.3 decision, not something a hash-chained log stored in the same database can solve alone.** A tamper-evident chain stored only inside the SQLite file it's protecting is itself rolled back along with the database if an attacker restores an older valid snapshot — detecting that requires a checkpoint anchored *outside* the rollback domain (e.g., an external append-only log, a periodically-exported chain digest, or similar). Step 5.3 must either design such an external anchor or explicitly document rollback detection as unsupported, rather than presenting a same-database hash chain as sufficient.

**Status**: Step 5.1 is approved as the threat-model baseline. Option B (§9) is a **provisional** architecture recommendation, not final — it awaits explicit approval of the intended caller model and an acknowledgment of the OS-identity-based authorization limitation in clarification #1 above, before Step 5.2 treats it as settled.

## 11. Explicitly out of scope for this document (per the approved Phase 2 boundary)

No API, schema, authentication/authorization implementation, audit-chain implementation, credential-storage implementation, or backup/restore implementation was designed or written here — only the threat model and trust boundaries those steps must design against. Per the approved scope: **stop after this document; wait for review and approval before Step 5.2.**
