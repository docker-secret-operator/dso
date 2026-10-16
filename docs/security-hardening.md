# Security hardening notes (Milestone 0)

This page documents the behavior introduced by the Milestone 0 security
hardening: REST/WebUI authentication, signed webhooks, vault write safety,
`dso export`, WebUI sessions and CSRF/Host protection. Everything here is
additive or has an explicit compatibility note.

## 1. REST API authentication

The REST API (`--api-addr`, default `127.0.0.1:8471`) decides who may call it as
follows:

| `DSO_AUTH_TOKEN` | WebUI (`webui.enabled`) | Anonymous request | Valid credential |
|---|---|---|---|
| unset | disabled | **allowed** (legacy mode, loopback only) | n/a |
| unset | enabled | **401** | session cookie |
| set | disabled | 401 | `Authorization: Bearer <token>` |
| set | enabled | 401 | bearer token **or** session cookie |

**Behavior change:** previously, enabling the WebUI without `DSO_AUTH_TOKEN`
left every `/api/*` route open to anyone who could reach the port, making the
login page decorative. Now an enabled WebUI always requires a credential.

Public routes (no credential): `GET /health`, `POST /api/auth/login`,
`POST /api/auth/logout`, the static WebUI shell (any non-`/api` path), and
`POST /api/events/secret-update` (which authenticates itself, see below).

Unchanged: DSO refuses to start the REST API on a non-loopback address without
`DSO_AUTH_TOKEN`, and the token must be 16-512 bytes.

**Client compatibility.** `docker dso logs --api` now sends
`Authorization: Bearer $DSO_AUTH_TOKEN` when that variable is set, and prints a
hint on `401`. A script that called the API anonymously while the WebUI was
enabled will now receive `401`; give it `DSO_AUTH_TOKEN`.

### Host and Origin checks

* **Origin (CSRF).** State-changing requests (`POST`/`PUT`/`PATCH`/`DELETE`) and
  the `/api/events/ws` upgrade that are *not* bearer-authenticated must be
  same-origin. If an `Origin` header is present its host must equal the request
  `Host` or an entry of `webui.allowed_origins`; `Origin: null` is rejected. If
  `Origin` is absent, `Sec-Fetch-Site: cross-site|same-site` is rejected.
  Requests with neither header (curl, scripts) pass: they are not CSRF vectors.
  Bearer-authenticated requests are exempt because browsers cannot attach that
  header cross-site (DSO sends no CORS headers). The client-side token in
  `web/lib/csrf.ts` is **not** validated by the server and is not relied upon.
* **Host (DNS rebinding).** `allowed_origins` and `allowed_hosts` are
  independent settings:
  * `allowed_origins` only widens the **CSRF** check above. It never turns on
    Host-header enforcement, so direct-by-IP access, Docker service names and
    external webhook senders using any hostname keep working.
  * Host-header enforcement is on when (a) the API is in **legacy open mode**
    (no token, WebUI disabled), where authentication cannot protect it, or
    (b) you set `webui.allowed_hosts`. While enforced, the `Host` must be a
    loopback name/IP or in the allowlist (otherwise `403`; `/health` is
    exempt). The allowlist is `allowed_hosts` plus the hosts of any
    `allowed_origins` entries (so one proxy origin entry also covers legacy mode
    behind that proxy). The effective list is logged at startup.
  * When a credential is required and `allowed_hosts` is unset, rebinding is
    already defeated by authentication (the attacker origin has neither the
    cookie nor the token), so no Host check is applied.
* **Reverse proxies.** If your proxy rewrites `Host`, list the public origin:

```yaml
webui:
  enabled: true
  allowed_origins: ["https://dso.example.com"]   # CSRF: browser origins allowed to POST
  allowed_hosts: ["dso.example.com"]             # optional: ENABLES Host enforcement
  trusted_proxies: ["10.0.0.5"]                  # EXACT proxy address(es): honor X-Forwarded-For for login throttling (see section 5)
  session_max_lifetime: 24h                      # absolute cap; "0" disables
```

* **Known limitation (deferred, Low): the same-origin comparison ignores the
  scheme.** An `Origin` of `http://dso.example.com` is accepted for a request to
  `dso.example.com`, because the server cannot see the external scheme behind a
  TLS-terminating proxy. Entries in `allowed_origins` ARE compared with their
  scheme. Mitigations in place: `SameSite=Strict` cookies (browsers treat an
  http page and an https site as cross-site for this purpose) and the `Secure`
  attribute. Closing it fully needs a trusted-proxy `X-Forwarded-Proto` signal;
  tracked as a follow-up.

## 2. Signed webhooks (`POST /api/events/secret-update`)

Enable and configure:

```yaml
agent:
  watch:
    webhook:
      enabled: true
      hmac_secret: "<32+ random bytes, e.g. output of: openssl rand -hex 32>"   # hard minimum 16; <32 logs a warning
      timestamp_tolerance: 5m        # default 5m, max 1h
      disable_legacy_auth: true      # recommended once senders are migrated
```

The webhook credential is independent of `DSO_AUTH_TOKEN`.

**Key strength.** `hmac_secret` must be at least 16 bytes (hard error below
that, unchanged). Between 16 and 31 bytes the configuration is accepted but
startup logs a warning with the length (never the value); generate 32 random
bytes with `openssl rand -hex 32`. DSO checks length only, not randomness, so a
guessable 16-character phrase is accepted: choose the key accordingly.

**Rotating the secret (deferred feature, operational procedure).** DSO holds one
shared key and has no overlap period (no "previous key" setting), so a rotation
is a coordinated cut-over: (1) generate the new key; (2) update the sender and
the agent's `hmac_secret`; (3) restart the agent. Deliveries signed with the old
key between steps are rejected with `401`; senders should retry with backoff and
re-sign each attempt. If the key may have been exposed (see section 6), also
treat any webhook accepted since the exposure as untrusted. A dual-key overlap
(`hmac_secret_previous`) is a possible follow-up; it was not added in this
milestone because it widens the accepted-key set and needs its own review.

### Request format

For the exact request body bytes `BODY` and the current Unix time `TS` (seconds):

```
X-DSO-Timestamp: <TS>
X-DSO-Signature: v1=<lowercase hex of HMAC-SHA256(hmac_secret, TS + "." + BODY)>
```

```bash
BODY='{"provider":"vault","secret_name":"db_password","event_type":"secret_updated","timestamp":"2026-01-01T00:00:00Z"}'
TS=$(date +%s)
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$DSO_WEBHOOK_HMAC_SECRET" -hex | sed 's/^.* //')
curl -sS -X POST "$DSO_URL/api/events/secret-update" \
  -H "X-DSO-Timestamp: $TS" -H "X-DSO-Signature: v1=$SIG" \
  -H 'Content-Type: application/json' --data-binary "$BODY"
```

Go senders can use `server.WebhookSignature(secret, ts, body)`.

### Verification and responses

| Condition | Response |
|---|---|
| Webhooks disabled / no usable credential configured | 403 |
| Missing/invalid timestamp, outside tolerance, bad/malformed signature | 401 |
| Timestamp earlier than the replay guard's start time (see "Lifecycle") | 401 |
| Body over 64 KiB (declared or streamed) | 413 |
| Valid signature already seen | 409 |
| Valid | request is processed |

Checks run cheapest-first and no body is buffered until the headers pass; the
body is read through a 64 KiB limit, and the MAC is compared in constant time.

### Replay protection

* **Storage:** an in-memory table of accepted signatures (the HMAC output
  identifies the exact timestamp+body pair), bounded to 10,000 entries.
* **Expiry:** an entry is kept until `timestamp + tolerance + 1s`, i.e. exactly
  as long as a replay of it could still pass the timestamp check.
* **Concurrency:** check-and-insert is one critical section, so of N concurrent
  deliveries of the same signed request exactly one is accepted.
* **Only authenticated requests are stored**, so unauthenticated traffic cannot
  fill the table. If the table is full of live entries new requests get 503
  instead of evicting one an attacker could then replay.
* **Lifecycle:** the guard (replay table and its floor) is created once, in
  `StartRESTServer`, **before the listener accepts any connection**. The floor
  is that moment and no request can move it; the guard is never reset while the
  process runs.
* **Replay across restarts (known limitation).** The replay table lives in
  memory only, so it is **empty after every restart**. As a partial
  compensation, any request whose timestamp is earlier than the new guard's
  start is rejected (`ts < floor`, whole seconds; `ts == floor` is accepted).
  When sender and agent clocks agree, this stops a request captured before the
  restart from being replayed into the restarted agent. It does **not** stop
  every case. A signed request captured before a restart can still be replayed
  into the restarted agent when both of these hold:
  1. the sender's clock was *ahead* of the agent's by more than the time between
     the capture and the restart, so the request's timestamp is at or after the
     new floor; and
  2. the replay happens before the request's timestamp ages out of the tolerance
     window (until roughly `timestamp + timestamp_tolerance`).

  In short, a pre-restart request is replayable only while the delay between
  capture and restart is *less than* the sender's clock lead. Samples measured
  with the default 5m tolerance (replay attempted 1s after the restart):
  synchronized clocks (lead 0): never replayable; lead 2s: replayable when the
  restart came 1s after capture, not at 20s; lead 30s: replayable at 1s and 20s,
  not at 60s; lead 120s: replayable at 1s, 20s and 60s, not at 200s; lead 290s:
  replayable at every tested delay up to 200s.

  *Practical exposure.* The attacker must capture a valid signed request (run
  webhooks over TLS), the sender's clock must be fast, and the agent must
  restart shortly after the capture. *Impact is bounded:* a replayed request
  carries no new secret material; it only makes the agent re-fetch the named
  secret from its provider and rotate if the value changed. DSO does not
  persist replay state in this milestone.

  A sender whose clock is *behind* the agent's by `d` seconds has requests
  rejected for the first `d` seconds after an agent start.

  *Recommendations.* Use TLS between sender and agent. Keep sender and agent
  clocks synchronized (NTP/chrony). A shorter tolerance shrinks the window:
  `timestamp_tolerance: 60s` is recommended **only if both clocks are
  synchronized to well within 60 seconds**; otherwise legitimate deliveries will
  be rejected as stale. The default stays at `5m`.
* **Signature casing:** the replay key is the decoded 32-byte MAC, so upper-,
  lower- and mixed-case hex of the same signature are the same delivery.
* **Uniqueness is the sender's job:** two byte-identical bodies signed within
  the same second produce the same signature and the second is a replay. Put a
  delivery id or timestamp in the JSON body.

### Legacy bearer mode (deprecated)

`auth_token` with `Authorization: Bearer <auth_token>` keeps working for one
release unless `disable_legacy_auth: true`. It has no body integrity or replay
protection. A startup warning and a once-per-process request warning are logged
(neither contains the token). It no longer needs to equal `DSO_AUTH_TOKEN`.
Rejection details log only a reason code and the peer address, never
credentials, signatures or bodies.

## 3. Vault write safety

* `Set`/`SetBatch` are one transaction under an exclusive `flock(2)` on
  `~/.dso/vault.enc.lock`: lock, **re-read the vault from disk**, apply, write,
  unlock. Concurrent CLI processes can no longer overwrite each other's keys.
  Waiting is bounded (30 s) and reports which lock timed out.
* Saves use a unique `0600` temp file in the same directory, `fsync`, atomic
  rename, then `fsync` of the directory. Any failing step returns an error and
  removes the temp file; a failed save leaves the previous vault untouched and
  leaves in-memory state unchanged. A vault that cannot be decrypted is never
  overwritten.
* **Failure after the rename.** The new vault becomes visible at the atomic
  rename; only the following directory `fsync` can still fail. That case
  returns a `*vault.DurabilityError` (`vault.IsCommitted(err)` is true): the
  update **is** on disk and in memory, but its durability across power loss is
  unconfirmed. Every earlier failure (create, write, fsync, rename) leaves the
  previous vault untouched and memory unchanged, and is not reported as
  committed.
* `InitDefault` runs under the same lock and installs the master key
  all-or-nothing (unique `0600` temp file, fsync, then `link(2)` to
  `master.key`, which cannot replace an existing key; filesystems without hard
  links fall back to `rename`). Concurrent `dso init` cannot produce a vault
  encrypted with a lost key, and a crash can no longer leave a partial or empty
  `master.key`. Leftover `.master.key.tmp-*` files from a crash are ignored.
* An **empty or whitespace-only** `master.key` (or blank `DSO_MASTER_KEY`) is
  rejected with a clear error instead of being used as an empty encryption key.
  If you hit this: with no `vault.enc` yet, delete the file and re-run
  `docker dso init`; if a `vault.enc` already exists, it may have been created
  under an empty key by an older version, so back both files up first.
* The on-disk format and key derivation are unchanged.
* Limits: the lock is advisory (it coordinates DSO processes, not hostile
  users) and `flock` is unreliable on NFS. **On non-Unix platforms the locking
  fallback is a no-op** (`pkg/vault/lock_other.go`): it provides no cross-process
  exclusion and concurrent writers can still lose each other's updates; Linux is
  the supported platform. `fsync` is the strongest guarantee POSIX offers; actual
  power-loss durability still depends on the filesystem and hardware.
  `Save()` writes the whole in-memory state and does not merge.

## 4. `dso export`

* New files are created `0600` with `O_EXCL`; an existing file (or symlink) is
  refused with a message pointing at `--overwrite`.
* `--overwrite` writes a new `0600` temp file and atomically renames it over the
  target, so a failed export never destroys the previous file and a symlink at
  the target is replaced, not followed.
* A failed export removes only the file it created.
* Output keys are sorted. Values containing newlines are written as-is
  (unchanged behavior); do not export such values to `.env` files.

## 5. WebUI sessions

* Session cookie `Max-Age` now follows the server: it equals the idle timeout,
  is capped by the remaining absolute lifetime, and is re-issued on every
  authenticated request (previously it was set once at login, so an active
  user's cookie expired at `login + idle timeout`).
* New absolute lifetime `webui.session_max_lifetime` (default `24h`, `0`
  disables). **Behavior change:** a session that has been continuously active
  for 24 h now ends; set `0` to keep the previous behavior.
* Login throttling is per client IP. `X-Forwarded-For` is honored only when the
  immediate peer is in `webui.trusted_proxies`, taking the right-most address
  that is not itself a trusted proxy. Without that setting, all clients behind
  a reverse proxy share one throttle bucket (by design: forwarded headers are
  never trusted by default).

### `trusted_proxies`: list exact proxy addresses

A listed peer's `X-Forwarded-For` is *believed*, so **every host inside a listed
range can choose its own throttle identity** and evade the per-client limit.
Therefore list the exact address(es) of your reverse proxy (`10.0.0.5`), not a
network.

| Configuration | Behavior (reproduced and covered by tests) |
|---|---|
| nothing / peer not listed | header ignored; throttled per connection address |
| exact proxy address | only that peer's header is believed; a direct client next to it cannot spoof |
| narrow range (/24 or smaller; IPv6 /120 or smaller) | accepted without a warning |
| broad range (wider than /24; IPv6 wider than /120) | accepted, **startup warning**; hosts inside the range can spoof (known limitation, pinned by a test) |
| `0.0.0.0/0` or `::/0` | **ignored**, with a startup warning; throttling stays per connection address |

Why match-all entries are ignored rather than trusted: Go never matches an IPv4
address against an IPv6 network (or the reverse), so before this fix a single-
family match-all let a client put hops from the *other* family in
`X-Forwarded-For`, fall outside the "trusted" set, and pick its own identity
(`0.0.0.0/0` with IPv6 hops, or `::/0` with IPv4 hops: 0 of 20 attempts
throttled in testing). Listing both families happened to fall back safely, but
no match-all entry identifies a proxy, so all of them are now dropped.

Deferred hardening (Low): a per-peer cap on distinct forwarded identities would
bound the damage of an over-broad range. It trades brute-force resistance
against availability (a distributed attack could then lock out the single
operator), so it was not added without an explicit decision.

## 6. Protecting the configuration file

The config (`/etc/dso/dso.yaml`, or `~/.dso/dso.yaml` in local mode) can hold
credentials: webhook `hmac_secret` / `auth_token`, the WebUI `password_hash`,
provider `auth.params` and `config` tokens, and notification URLs.

**Permissions policy.** DSO creates and rewrites the file with mode `0640`
(owner read/write, group `dso` read-only, no access for others), in agreement
with `dso doctor` check DSO-DOCTOR-009 (accepts `0600` or `0640`, flags any
world bit or group-write bit). Local mode already used `0600`.

* New installs and re-runs of bootstrap write the file atomically (temp file in
  the same directory, then rename) with that mode, so the new content is never
  visible with a looser one.
* Rolling back a failed bootstrap restores the previous content with the
  previous mode, **but never looser than `0640`**.
* Permission setup is **tighten-only**: an existing `0664`, `0644`, `0660` or
  `0666` file becomes `0640`; an already stricter file (`0600`, `0400`) is left
  exactly as it is. Only the DSO-managed path `/etc/dso/dso.yaml` is touched.
* **Behavior change:** group members of `dso` can no longer edit the file
  directly (group-write was removed because the file holds secrets); use `sudo`.
  The `/etc/dso` directory mode is unchanged (`0775`): a group member can still
  replace the file by renaming over it, so treat `dso` group membership as
  trusted.

**Existing installations are NOT changed by upgrading the binary.** The new mode
is applied when bootstrap/permission setup is re-run. To remediate by hand:

```bash
sudo chown root:dso /etc/dso/dso.yaml     # if your install uses the dso group
sudo chmod 0640 /etc/dso/dso.yaml
ls -l /etc/dso/dso.yaml                   # verify: -rw-r----- root dso
```

`dso doctor` reports a file that is still too permissive. If a config that
contained `hmac_secret`, `auth_token` or provider credentials was ever
world-readable, treat those credentials as exposed and rotate them.

**`dso config show` redacts credentials.** It prints the file with credential
values replaced by `"[REDACTED]"` (keys, comments and everything else are kept;
unset credentials stay visibly empty). Redaction is schema-aware: fields tagged
`dso:"secret"`, `dso:"url"` (keeps scheme/host/port, drops userinfo, path and
query) or `dso:"secret-by-key"` in `pkg/config`, plus a name-based backstop
(`secret`, `token`, `password`, `credential`, `api_key`, `access_key`,
`private_key`, `hmac`, `bearer`, `dsn`) for keys the schema does not define.
A file that is not valid YAML is **refused** rather than printed raw. A test
fails if a credential-looking field is added to the schema without a tag.
To see a value, open the file directly with sufficient permissions.
