# Decision Record: Configuration-File Permission Policy (P-01)

**Date:** 2026-10-24
**Status:** Recommendation only — no policy or code change made to existing `dso.yaml` enforcement in this task, per explicit instruction.
**Related:** `docs/reviews/2026-10-24-dso-complete-product-audit.md` (P-01), `docs/reviews/2026-10-24-phase0-phase1-security-findings-register.md`

## Current behavior (verified, unchanged)

- `pkg/config/permissions.go:23-38` (`PermissionAdvisory`): read-only check, surfaced via `dso config` and `dso agent` (callers: `internal/cli/config.go:94`, `internal/cli/agent.go:103`). Never modifies the file. Explicitly documented in its own doc comment as intentional: "the path may be user-supplied and chmod-ing it behind the operator's back could break their deployment."
- `internal/bootstrap/permissions.go`: a **fresh bootstrap install** sets `/etc/dso/dso.yaml` to `0640` and tightens an existing file toward that bound. `VerifyPermissions` (lines 398-471), used for post-install/`doctor`-style checks, only logs `Warn` for an insecure file — it never returns an error and never blocks startup (`return nil` unconditionally).
- Net effect: **advisory-only everywhere**, at install time, at config-load time, and at verify time. No DSO code path currently refuses to start or refuses to read `dso.yaml` because its permissions are too permissive.

## Why existing deployments may rely on this

- `dso.yaml` predates the `0640` convention in some installs — the code comments note bootstrap "used to create /etc/dso/dso.yaml as 0664/0644" historically. An operator who has been running DSO with a looser mode (e.g., because their deployment tooling manages ownership itself, or because of an older install) would have their agent **refuse to start** if this were made fail-closed today, with no migration path other than manually fixing the file before upgrading.
- `dso.yaml`'s path is operator-configurable (via `--config`/env var), so DSO cannot assume it always controls the file's creation — unlike `internal/crypto`'s key files, which DSO itself always creates via `securefs`'s atomic, exclusive-install path.

## Risk of the current permissive-but-advisory approach

- `dso.yaml` holds the WebUI password hash and provider credentials (Vault AppRole secret_id, cloud API keys — see SECURITY.md:321-326, THREAT_MODEL.md:27). A world- or group-readable `dso.yaml` on a multi-user host exposes all of this to any local user who can read it, with only a printed warning (easy to miss in non-interactive/systemd-managed startups) standing between the insecure state and continued operation.
- This is an **inconsistency**, not just a risk in isolation: key material gets a fail-closed guarantee (`securefs.ReadPrivateFile` rejects insecure permissions/ownership on open — confirmed in the prior security audit's F-06); the config file holding equally sensitive credentials does not. An operator reading only the crypto code could reasonably over-trust the config file's protection by false analogy.

## Recommendation for *new* central-store credential material

`dso central`'s own credential/config storage is **new code with no existing-deployment compatibility constraint** — unlike `dso.yaml`, there is no installed base whose startup would break from day-one enforcement. Recommendation:

1. **Default to fail-closed permission enforcement for any new central-store credential file**, mirroring the existing `securefs` key-file convention (reject on open if group/world-readable or owned by someone other than the expected user, rather than only warning).
2. **Do not retrofit this onto the existing `dso.yaml` loader** without a separate, explicit migration plan (e.g., an opt-in `--strict-config-permissions` flag with a deprecation timeline, surfaced in `dso doctor` and release notes well before any default change) — changing `dso.yaml`'s behavior to fail-closed silently would be a breaking change for any deployment currently relying on the advisory-only behavior, which is exactly the compatibility risk the original code comment already identifies and the task's own constraints (no policy change without approval) correctly guard against.
3. If `dso central`'s credential file is ever meant to be optionally *compatible* with a pre-existing, looser-permission file (e.g., during a migration from the legacy single-`dso.yaml` model), that compatibility path should itself default to the advisory/warn behavior only for the *legacy* file format, while the *new* central-store-native format defaults to fail-closed from its first release.

## Compatibility and migration implications

- Fail-closed-by-default for new central-store files: no migration burden, since no existing deployment has that file yet.
- Any future move to make `dso.yaml` itself fail-closed would need: a deprecation window, a `dso doctor` check that proactively flags and offers to fix the mode (`chmod`) with explicit operator consent, and release-notes visibility — none of which is in scope for this task and none of which was implemented here.

## Explicitly not done in this task

- No change was made to `pkg/config/permissions.go`'s advisory behavior.
- No change was made to `internal/bootstrap/permissions.go`'s `VerifyPermissions` warn-only behavior.
- No new fail-closed enforcement was added to the existing config loader.

This record is a recommendation for the product/engineering decision that should precede any `dso central` credential-storage implementation (Phase 2) — it does not authorize starting that implementation.
