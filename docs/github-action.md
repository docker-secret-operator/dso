# DSO Validate — GitHub Action

A thin CI wrapper around `docker dso validate`. It installs a specific,
signature-verified DSO release and runs `docker-dso validate` against your
Compose project — it does not implement any validation logic of its own.
Everything it reports is exactly what `docker dso validate` would report if
you ran it locally.

**The action executes `dso validate` from the DSO release you select via
`version`. Which flags and checks are available is therefore determined
entirely by that release, not by this action.** In particular, `--json`,
`--file`, and the Compose/reference/secret-existence checks described
below require a DSO release that includes them — see
[Version pinning](#version-pinning).

```
GitHub Action
     │
     ├── install a signature-verified DSO release
     │
     └── docker-dso validate
              │
              ├── stdout  → validation result (text, or JSON with --json)
              └── exit code → workflow success/failure
```

## Usage

```yaml
name: DSO Validation

on:
  pull_request:

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: docker-secret-operator/dso/.github/actions/validate@vX.Y.Z
        with:
          version: vX.Y.Z
```

Replace `vX.Y.Z` with a real DSO release tag (see [Version pinning](#version-pinning)
below — the action reference and the `version` input are pinned
independently and don't need to match).

## Inputs

| Input | Required | Default | Description |
|---|---|---|---|
| `version` | yes | — | DSO release tag to install, e.g. `v4.0.0` (use a release that includes the `validate` features you rely on — see [Version pinning](#version-pinning)). The literal value `latest` is supported as an explicit opt-in to always install the newest release. Never resolves to `main`/`master` or any unpinned branch. |
| `working-directory` | no | `.` | Directory (relative to the job's default working directory) to run `docker-dso validate` in. |
| `args` | no | `''` | Additional arguments forwarded verbatim to `docker-dso validate`, e.g. `--json` or `--file docker-compose.dso.yml`. Only flags `docker-dso validate --help` already documents are meaningful here — the action does not add, translate, or duplicate any validation behavior. |

## Outputs

| Output | Description |
|---|---|
| `dso-path` | Absolute path to the installed `docker-dso` binary, if a later step needs to invoke it directly. |

## Version pinning

CI pipelines should pin an explicit version:

```yaml
with:
  version: vX.Y.Z
```

**Pin to a release that actually contains the `validate` behavior you
need.** The action does not add fallback logic for older releases that
predate a given `validate` flag or check — an older, pinned release simply
runs its own, older `validate`. This is deliberate: silently
papering over a version mismatch would hide exactly the kind of drift a
CI validation gate exists to catch. If a workflow's `args` (e.g. `--json`,
`--file`) stop working after pinning to an older release, that means the
release you selected predates those features — pin a newer one instead of
expecting the action to compensate.

`latest` is supported as an explicit, documented opt-in — it resolves to
the newest published release via the GitHub Releases API at the start of
each run, not a moving branch reference:

```yaml
with:
  version: latest
```

This is less deterministic (a new DSO release can change what "latest"
means between runs) and is not recommended for production release
pipelines.

**Compatibility note**: this action wraps whatever `docker-dso validate`
that release provides. `validate`'s `--json` output, `--file` flag, and the
Compose/reference/secret-existence checks described in this document were
added as part of DSO's CLI Phase 1 work. Pin a version that includes that
work if you rely on `--json` or those checks; earlier releases only
support the plain-text, configuration-only `validate` behavior.

## Working directory

```yaml
- uses: docker-secret-operator/dso/.github/actions/validate@vX.Y.Z
  with:
    version: vX.Y.Z
    working-directory: ./deploy
```

Only affects where `docker-dso validate` looks for a Compose file and DSO
config — nothing is checked out, generated, or modified outside that
directory.

## Arguments and JSON output

```yaml
- uses: docker-secret-operator/dso/.github/actions/validate@vX.Y.Z
  with:
    version: vX.Y.Z
    args: --json
```

`docker-dso validate`'s stdout, stderr, and exit code all pass through
unmodified — the action never parses or reinterprets the CLI's output, and
never prepends anything to stdout that could break JSON parsing.

## Failure behavior

`docker dso validate` exits `0` when the project is valid and non-zero
when at least one check fails. The action does not convert failures into
warnings and does not swallow the exit code — a failing validation fails
the workflow.

## Provider authentication

If your `dso.yaml` configures a cloud provider (AWS, Azure, Vault), that
provider's credential checks run as part of validation the same way they
would locally. Supply credentials via GitHub's own secrets mechanism
(`secrets.*` → `env:` on the job or step) — never hard-code them in the
workflow file, and never pass them as `args`, since command arguments can
end up in logs.

```yaml
- uses: docker-secret-operator/dso/.github/actions/validate@vX.Y.Z
  with:
    version: vX.Y.Z
  env:
    AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
    AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
```

`docker-dso validate` never prints secret values, provider credentials, or
`.env` contents — only whether checks passed, and (for missing secrets)
which key name was not found.

## Security model

The installer (`install-dso.sh`) never runs `curl | sh` and never executes
an unverified binary:

1. Download the release's `checksums.txt`, its cosign signature (`.sig`),
   and signing certificate (`.pem`).
2. Verify the signature with `cosign verify-blob`, pinned to the exact
   identity of `docker-secret-operator/dso`'s own release workflow
   (`.github/workflows/release.yml`, issued by
   `https://token.actions.githubusercontent.com`). A checksums file that
   doesn't carry a valid signature from that exact workflow is rejected —
   checksums alone are not treated as proof of authenticity.
3. Only once the checksums file is proven authentic, download the
   platform-matched release archive and verify its SHA-256 against that
   file.
4. Extract and install.

This reuses the same `cosign` installation mechanism
(`sigstore/cosign-installer`) that DSO's own release pipeline uses to sign
releases in the first place — not a separately-maintained verification
path.

## Supported platforms

DSO publishes release binaries for Linux and macOS, on amd64 and arm64
(see `.goreleaser.yml`) — no Windows artifacts exist. The action fails
with a clear error on an unsupported runner OS/architecture rather than
attempting a download that cannot succeed.

## Limitations

- No Windows runner support (matches DSO's own release artifacts).
- `latest` resolution calls the unauthenticated GitHub Releases API,
  which is subject to GitHub's standard unauthenticated rate limits.
- The action installs and runs the CLI's `validate` command only. It does
  not run `doctor`, `migrate`, `up`, or any other DSO command.
