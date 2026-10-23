# Independent security and architecture review — DSO M0 and M1

Date: 2026-10-16 · Reviewer: Claude (independent pass; no application code, design docs or git state changed)

**Verdict: FIX REQUIRED** — no Critical or High findings; three Medium M1 findings
and three Medium M0 findings should be fixed before M2 builds on this base
(details and ordering in §6). Nothing found requires BLOCK.

## 0. Scope, state and method

| | Path | Branch | Commit | Status |
|---|---|---|---|---|
| Original | `…/dso` | `feature/webui-phase1` | `9c9717d` | 57 pre-existing WIP entries, unchanged |
| M0 | `…/dso-m0` | `security/milestone-0-hardening` | `cc0e070` | clean |
| M1 | `…/dso-m1` | `feature/m1-storage-keys` | `9c03c1c` | clean (this report is the only new, uncommitted file) |

Ancestry verified: `9c9717d` → `cc0e070` → `9c03c1c`. Paths/branches/hashes matched
the expected values, so the review proceeded.

Method: read the committed code (not the changelog/reports) for the areas listed
in the request; wrote **throwaway reproduction tests in a scratch copy**
(`/tmp/claude-1002/review`, copied from the M1 worktree, never inside any repo)
to confirm or refute suspicions; ran static tools and the test suites in the
worktrees. Findings are labelled **CONFIRMED** (reproduced by a run, or
unambiguous from the cited lines), **SUSPECTED** (reasoned, not exercised) or
**ACCEPTED** (documented limitation). Passing tests are treated as evidence, not proof.

Not reviewed in depth (stated so absence of a finding is not read as assurance):
the pre-existing WebUI front end, provider plugins, the Docker/rotation code,
`pkg/vault/crypto.go` internals beyond what M0 touched, and the full body of
every M0 test file.

## 1. Verification commands and results (actually run)

| Command (cwd) | Result |
|---|---|
| `git diff --check 9c9717d cc0e070` (M0) | rc=0, no output |
| `git diff --check cc0e070 9c03c1c` (M1) | rc=0, no output |
| `go vet ./...` (M0) | rc=0 |
| `go vet ./...` (M1) | rc=0 |
| `go test -race -count=1 ./pkg/config ./pkg/vault ./internal/webuiauth ./internal/server ./internal/bootstrap ./internal/auth` (M0) | rc=0 (webuiauth 318 s, server 181 s, vault 139 s) |
| `go test -race -count=1 ./internal/crypto ./internal/store/... ./internal/centralsvc ./internal/cli` (M1) | rc=0 |
| `go test ./... -count=1` (M1, includes all M0 code) | rc=0, 39 packages `ok`, no FAIL |
| `GOOS={windows,darwin,freebsd} CGO_ENABLED=0 go build ./internal/crypto ./internal/store/... ./internal/centralsvc ./pkg/vault` (scratch) | all rc=0 (compiles; behaviour on those OSes **not** run) |
| `govulncheck` on `internal/{crypto,store,centralsvc,cli}` (M1) | 15 reachable vulnerabilities, all in **pre-existing** dependencies: stdlib `net/http`, `net/textproto`, `crypto/tls`, `html/template` (go1.25.13), `golang.org/x/net v0.55.0`, `google.golang.org/grpc v1.82.1`, `github.com/docker/docker v28.5.2` (no fix). None in `modernc.org/*`. Not compared against a baseline run, so "pre-existing" rests on M1 adding only `modernc.org/{sqlite,libc,mathutil,memory}` and indirect helpers. |
| `gosec` on the new M1 packages | 15 issues: 9× G104 (unchecked `Close` on error paths, benign); 4× G304 (variable file path, intended); G101 (false positive: SQL column names); G115 `envelope.go:116` `uint32(len(s))` (fields are short; benign) |
| `go mod tidy -diff` (scratch) | non-empty: `modernc.org/sqlite` is marked `// indirect` (see M1-7) |
| Reproduction tests (scratch only) | see per-finding evidence |

## 2. M0 findings

### M0-1 Medium — CONFIRMED: login throttle is bypassable by concurrency
`internal/webuiauth/loginlimiter.go:286` checks `allowed(key)` and only at `:292`
records the failure *after* bcrypt has run. Concurrent requests all pass the
check before any failure is recorded.
**Evidence:** 40 concurrent wrong-password logins from one address with a
budget of 5 → **29 reached bcrypt**, 11 throttled.
**Impact:** online guessing is faster than the stated 5-per-15-min budget. The
global per-IP HTTP limiter (`rateLimitMiddleware(10, 30)` in `rest.go:1189`) caps
a burst at roughly 30, which limits but does not remove the effect; the
reproduction bypassed the limiter by calling the manager directly.
**Fix:** reserve the attempt atomically (increment before bcrypt, decrement on
success), or serialize per-key.

### M0-2 Medium — CONFIRMED: throttle key is the full IPv6 address
`clientKeyFromRequest` (`loginlimiter.go:122`) keys on the literal IP.
**Evidence:** 50 addresses inside one /64, 5 guesses each → **250 guesses reached
bcrypt** (a single address allows 5).
**Impact:** an attacker holding any IPv6 prefix (a /64 is standard) is effectively
unthrottled; the map also grows by one entry per address between 5-minute sweeps.
**Fix:** key IPv6 by /64 (or /56), and add a global failure ceiling / backoff.

### M0-3 Medium — CONFIRMED: `dso config show` redaction is name-driven for free-form maps
`pkg/config/redact.go:25` (`credentialKeyPattern`) decides by key name for
`providers.*.config` and `legacy` maps.
**Evidence (scratch run of `RedactYAML`):** `endpoint: "https://admin:hunter2pass@vault.internal:8200"`,
`connection: "postgres://svc:dbpassword99@db/app"`, a JWT under `jwt:`, and
`role_id:` were all printed verbatim; `hmac_secret` was redacted.
**Impact:** credentials embedded in values under non-credential-looking keys leak
to terminals, tickets and logs. Schema-tagged fields (hmac_secret, auth_token,
password_hash, webhook URL) are covered.
**Fix:** additionally redact by *value* shape (URL userinfo, JWT, long
high-entropy strings) regardless of key, or switch provider config to an allowlist
of known non-secret keys; document the residual risk.

### M0-4 Medium — CONFIRMED (environment) / Low (code): upgrade path does not fix existing installs
`internal/bootstrap/permissions.go:283-352` (tighten-only 0640) runs only when
bootstrap runs. **Evidence:** `stat /etc/dso/dso.yaml` on this machine →
`644 root:root`; M0 did not (and was told not to) change it, and nothing at agent
or server start refuses or warns about a world-readable config (the doctor check
reports it). A config holding `hmac_secret` therefore stays world-readable until
an operator re-runs bootstrap or chmods manually (documented in
`docs/security-hardening.md`).
**Fix:** warn loudly at server/agent start when the config is group-other
readable and contains credentials; optionally offer a `dso doctor --fix`.

### M0-5 Low — CONFIRMED: webhook legacy bearer is a downgrade path while HMAC is configured
`internal/server/webhook_auth.go:223-243`: when `hmac_secret` and the legacy
`auth_token` are both set and `disable_legacy_auth` is false, a request that
simply omits `X-DSO-Signature` is accepted on the legacy token alone — no
timestamp, no body binding, no replay protection.
**Impact:** whoever holds only the legacy token (the weaker credential) bypasses
the HMAC/replay controls. Documented as deprecated; default stays permissive for
compatibility.
**Fix:** default `disable_legacy_auth` to true once an `hmac_secret` is set (or
log a startup *warning* naming this exact downgrade).

### M0-6 Low — ACCEPTED/CONFIRMED: replay and origin limits
* Replay table is in-memory with a process-start floor (`webhook_auth.go`): sound
  for one process; a clock stepped backwards after start, multiple instances behind
  a load balancer, or a request timestamped in the exact start second are not
  covered. Documented.
* `originAllowed` compares host, not scheme (`auth_gate.go`); requests with
  neither `Origin` nor `Sec-Fetch-Site` are accepted. Browsers always send one of
  them on cross-site writes, so the residual risk is old browsers (documented).

### M0-7 Low — CONFIRMED by reading: limiter housekeeping
`loginlimiter.go`: one never-stopped cleanup goroutine per limiter; entries are
swept every 5 min only. Bounded by request rate; mostly hygiene.

### M0-8 Informational — carried over, not introduced by M0
* `pkg/vault/crypto.go` was not changed by M0: Argon2id runs on every load and
  ciphertext binds no AAD (design finding F3 is still open).
* `getMasterKey` (`vault.go:45-70`) follows symlinks and silently `chmod 0600`s
  the target; no ownership check. `DSO_MASTER_KEY` is read from the environment
  (visible in `/proc/<pid>/environ` to same-user/root).
* The vault `checksum` check is skipped when the field is absent
  (`vault.go:248-275`); AES-GCM already authenticates, so this is cosmetic.
* On non-unix platforms the vault lock is a documented no-op
  (`pkg/vault/lock_other.go`).

### M0 areas checked with no defect found
Every route in `rest.go` `ServeHTTP` passes the auth gate except `/health`,
login, logout and the self-authenticating webhook; webhook MAC is constant-time,
signed string is `ts "." body`, hex decoded before keying the replay table, body
capped at 64 KiB, timestamp window and restart floor enforced before the body is
buffered; session cookie is HttpOnly + SameSite=Strict + Secure (configurable),
absolute lifetime enforced, token 256-bit random, new token on every login;
login CSRF is covered by the origin check; `/0` trusted-proxy entries are
ignored. **WIP interaction:** `cc0e070` contains no rotate or notification-test
handler (`grep` of `rest.go` shows only a comment at line 609), and the M0 tree
builds and passes without the WIP files.

## 3. M1 findings

### M1-1 Medium — CONFIRMED: the "pre-migration backup" can be stale
`internal/store/sqlite/sqlite.go:241-246`: if `<db>.pre-migration-v<N>.bak`
already exists the function returns nil without making a new copy.
**Evidence:** migration attempt 1 fails (backup of v1 taken); the operator keeps
writing at v1; attempt 2 succeeds → the backup **lacks** the data written
between the attempts. This contradicts `docs/central-store-m1.md` ("a restorable
copy is made before changing a non-empty database").
**Fix:** unique name per attempt (timestamp/sequence) or refuse with an error
telling the operator to move the old file.

### M1-2 Medium — CONFIRMED: the database is not bound to its root key
`internal/centralsvc/service.go:84` (`CreateProject`) wraps the new DEK under
whatever provider is configured; `service.go:156` only compares the key id per
project on read.
**Evidence:** on an initialised database, a service built with a *different*
root key created a project successfully → a mixed-key database where some
projects are readable only with key A and others only with key B.
**Impact:** an operator mistake or a substituted key file silently forks custody;
secrets written afterwards are protected by the wrong (possibly attacker-chosen)
key and the original key can no longer open them.
**Fix:** record the root key id (and an encrypted canary) in a `store_meta` row at
`init` and refuse every write when the provider's id differs.

### M1-3 Medium — CONFIRMED: parent-directory permissions are never checked
`internal/crypto/keyprovider.go:148-185` and `sqlite.go:139-170` validate the
file but not its directory.
**Evidence:** a 0400 key file in a 0777 directory is accepted, and so is a key
that another user replaced there. With M1-2 this lets a local user with write
access to the directory substitute the key (new projects get wrapped under the
attacker's key) or swap the database.
**Fix:** require key/db directories to be owned by the user and not group/other
writable (or sticky), mirroring the file checks; resolve symlinks first.

### M1-4 Low — CONFIRMED by reading: `init` leaves recoverability gaps
* `installExclusive` (`keyprovider.go:106-140`): the temp file holds the full root
  key; a crash between `CreateTemp` and the deferred `os.Remove` (line 116)
  leaves a second `0400` copy `.dso-key-*` beside the key, and a failed `Remove`
  after `link` leaves two hard links.
* `dso central init` (`internal/cli/central.go`) installs the key *before* the
  database; if opening the DB then fails, re-running `init` is refused
  ("already exists") and the operator must delete the key by hand.
**Fix:** sweep stale `.dso-key-*` on init, or create the DB first / make `init`
resumable.

### M1-5 Low — CONFIRMED by reading: CLI alters stdin values
`central.go:302` strips one trailing `\n` (CRLF input keeps `\r`). Values that
legitimately end in a newline need an extra blank line. `io.ReadAll` growth
leaves unzeroed copies in memory (best effort only, documented).
**Fix:** add `--raw` (no trimming); document.

### M1-6 Low (design) — CONFIRMED by reading: disabling the latest version silently serves an older one
`sqlite.go:575` selects the highest *enabled* version. Disabling a leaked v3 makes
consumers receive v2, which may also be revoked. The design used an explicit
`current_version_id`.
**Fix:** require an explicit "current" pointer, or return `ErrNoActiveVersion`
unless the caller names a version.

### M1-7 Low — CONFIRMED: dependency hygiene
`go.mod:152` lists `modernc.org/sqlite v1.50.0 // indirect` although it is
imported directly (`go mod tidy -diff` non-empty). The driver pulls
`modernc.org/libc` (a large transpiled-C runtime) — a supply-chain and
binary-size cost accepted in the design. Run `go mod tidy`; pin and review.

### M1-8 Low — SUSPECTED (not run on those OSes)
Cross-compiles for windows/darwin/freebsd, but: on Windows `os.FileMode` reports
broad bits so the `&0o077` checks (`keyprovider.go:161`, `sqlite.go:~165`) would
refuse every key/db (fail closed, unusable); `O_NOFOLLOW`/ownership are skipped on
non-unix; the doc says the platforms "compile but skip ownership checks", which
understates this. The vault file lock is a no-op there (M0-8).

### M1-9 Low — CONFIRMED by reading
`--passphrase-file` is validated with `os.Stat` (follows symlinks, no ownership
check, TOCTOU) at `central.go:72`; a passphrase stored in a file reduces the
passphrase provider to file-provider strength (documented). The colocated-key
guard (`central.go:166`) compares `filepath.Abs` paths without resolving
symlinks and cannot see separate volumes (documented).

### M1-10 Informational — cryptographic design notes (no defect)
* **Nonces:** 96-bit `crypto/rand` per seal, entropy failure is an error
  (`envelope.go`); 20 000 seals gave no repeat (test). NIST's 2³² messages-per-key
  guideline bounds a single project DEK; far above realistic use, but there is no
  counter or rotation trigger.
* **AAD:** project, environment, secret id, **name**, version and dek-version,
  length-prefixed; each field is covered by a failing substitution test, and
  mutation testing shows dropping any one is detected (the secret-id mutant
  initially survived and a test was added). Not authenticated: `comment`,
  `created_by`, timestamps (immutable only by trigger). The name in the AAD means
  a future rename must re-encrypt.
* **Key lifecycle:** DEK random 256-bit, wrapped with AAD (project, key id,
  dek version), zeroed after use (best effort). The row's `dek_version` is not
  compared with the project's current one — fine today, a trap once DEK rotation
  exists.
* **Argon2id:** t=3, 64 MiB, p=4, 16-byte salt; loader enforces floors (t≥2,
  ≥19 MiB) and ceilings (t≤12, ≤1 GiB); tampered parameters are rejected (tests).
  `key_id` is a SHA-256-derived check value, i.e. an offline passphrase verifier
  that costs one Argon2id per guess. Minimum passphrase length 12, no entropy check.
* **SQLite:** WAL, `synchronous=FULL`, `foreign_keys=ON` verified on **all 8
  pooled connections** (scratch run), immediate transactions, parameterised SQL
  throughout (`VACUUM INTO ?` included); panic inside the seal callback rolls
  back (a test found and fixed a lock-wedge bug). 8 processes opening a fresh
  database simultaneously: 0 of 40 failures over 5 rounds. A SIGKILL mid-write test
  passes `integrity_check`. Not tested: ENOSPC, fsync failure, network
  filesystems — **SQLite WAL must not be used on NFS/SMB; this is undocumented.**
* **Immutability triggers** block edits/deletes of version rows and re-enabling;
  `secrets`, `projects.wrapped_dek` and similar rows are not trigger-protected,
  which is covered by the AAD checks (tests drop the triggers and still fail closed).
* **Plaintext trace (CLI → service → store → output):** values enter only via
  hidden prompt or stdin (no flag exists — tested); live as `[]byte` in the
  service; leave as ciphertext; the service logs ids/names/versions/actor only
  (test asserts no plaintext or key in logs, DB, WAL, metadata JSON or `%v`
  formatting); errors are generic (`ErrDecrypt`, `ErrIntegrity`). Plaintext is
  printed only by `get --reveal` (terminal scrollback is the caller's risk). Not
  covered: core dumps/swap (no `mlock`), shell `echo secret | …` history by the
  operator, and `/proc/<pid>/environ` (no secret travels via env).
* `.gitignore:72` contains a bare `secrets/` rule that silently ignores any
  future `…/secrets/…` directory (it already forced the service package to be named
  `centralsvc`).

## 4. Design-document inconsistencies

1. **Rollback:** the design copies a ciphertext reference; with the version in the
   AAD that cannot authenticate. Rollback must decrypt and re-encrypt. Fix the design.
2. **Rename:** name and secret id are both in the AAD; rename = re-encrypt (or drop
   the name from the AAD). The M1 task asked for the name; the design only the id.
3. **Audit key via HKDF from the root key:** impossible with the implemented
   `KeyProvider` (Wrap/Unwrap) for a KMS, whose key cannot be exported or
   HKDF-derived. Needs a stored, wrapped random audit key (or a provider
   `Derive`) — decide before M2.
4. **Root-key placement:** design says a different path *or volume*; M1 checks only
   directory difference (and no symlink resolution, M1-9). Docs state this.
5. **Backup/restore, import-vault and key-rotation** are in the design's M1 list
   and absent. `dek_version`/`dek_key_id` leave room for rotation.
6. **Windows/other OS** statement in `docs/central-store-m1.md` understates M1-8.
7. **"Restorable copy before migration"** claim is false in the stale-backup case (M1-1).

## 5. Deferred items: which are actually required

| Item | Verdict |
|---|---|
| Root-key pin / canary in the DB | **Required for M1 correctness** (M1-2) — not a later feature |
| Directory-permission validation | **Required** (M1-3) for the stated threat model |
| Root-key backup/escrow procedure + `init` confirmation | Required before *any* real data (loss = total loss); acceptable to defer while only test data is stored |
| Backup/restore/verify commands | Required before production; can follow M2 |
| Root/DEK rotation | Needed before production; schema is ready |
| Purge of soft-deleted secrets | Safe to defer |
| AAD completeness for `comment`/metadata | Safe to defer |
| Authn/RBAC/audit chain | Correctly M2 |

## 6. Prioritised summary and release blockers

| # | Sev | Area | Status |
|---|---|---|---|
| M1-2 | Medium | M1 root key not pinned to DB | CONFIRMED |
| M1-3 | Medium | M1 directory perms unchecked | CONFIRMED |
| M1-1 | Medium | M1 stale migration backup | CONFIRMED |
| M0-1 | Medium | M0 login-throttle race | CONFIRMED |
| M0-2 | Medium | M0 IPv6 throttle evasion | CONFIRMED |
| M0-3 | Medium | M0 config-show value leaks | CONFIRMED |
| M0-4 | Medium/Low | M0 installed config still 0644 | CONFIRMED |
| M0-5, M1-4…M1-9, M0-6/7 | Low | see above | mixed |
| M0-8, M1-10 | Info | see above | — |

**Release blockers for any production claim:** M1-2, M1-3, M1-1, M0-1, M0-2,
M0-3, root-key backup procedure, plus the pre-existing dependency vulnerabilities
reported by `govulncheck` (stdlib/x/net/grpc/docker — outside M0/M1 but reachable).
**Not blockers for starting M2 design:** items marked Low/Informational, provided
M1-2 and M1-3 are fixed first because M2 will store credentials on this base.

## 7. Final verdict

**FIX REQUIRED.** The cryptographic core (AEAD use, AAD, nonce handling, key
wrapping, provider parsing, Argon2id validation, transactions, immutability) held
up under review and adversarial tests. The gaps are in operational binding of
key to database and directory trust (M1-2/3), a misleading backup guarantee
(M1-1), and three M0 hardening gaps (throttle race, IPv6 key, value-shape
redaction). None is exploitable remotely without credentials on the current,
local-only M1 surface.
