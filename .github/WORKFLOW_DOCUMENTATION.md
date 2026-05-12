# GitHub Actions Workflow Documentation

This document explains the CI/CD workflows for DSO (Docker Secret Operator).

---

## Overview

DSO has two GitHub Actions workflows:

1. **CI Workflow** (`.github/workflows/ci.yml`)
   - Runs on: push to `main`, pull requests to `main`
   - Jobs: Build, Test, Lint, CLI Guard, Security Scan, DCO Check

2. **Release Workflow** (`.github/workflows/release.yml`)
   - Triggers: Push of tags matching `v*` (e.g., v1.0.0, v1.2.3-rc1)
   - Jobs: Build & Release with GoReleaser

---

## CI Workflow (`.github/workflows/ci.yml`)

### Purpose
Validate code quality, security, and functionality on every push and pull request.

### Jobs

#### 1. Build & Test
**Runs on:** Ubuntu latest, Go 1.25.0

```yaml
build:
  - Checkout code
  - Setup Go (from go.mod version)
  - Verify go.mod is tidy (no unchecked dependencies)
  - Build: make build → produces docker-dso binary
  - Test: go test with race detector
```

**Failure scenarios:**
- `go.mod` or `go.sum` out of sync
- Build fails to produce `docker-dso` binary
- Tests fail with race condition detected

**Fix:** Run locally before pushing:
```bash
go mod tidy
make build
go test ./...
```

#### 2. Lint
**Runs on:** Ubuntu latest

Uses `golangci-lint` with configuration:
- Action: `golangci/golangci-lint-action@v6`
- Version: Latest
- Timeout: 5 minutes
- Skips: `test` directory

**Failure scenarios:**
- Code style violations
- Unused variables/imports
- Security issues detected by linters

**Fix:**
```bash
golangci-lint run ./...
# or auto-fix some issues:
gofmt -w .
```

#### 3. CLI Guard
**Purpose:** Validate that CLI usage is consistent with Phase 1-6 design

Runs: `scripts/validate-cli.sh`

Checks:
- No legacy command patterns
- Proper Docker plugin syntax: `docker dso <command>`
- Prevents accidental introduction of deprecated commands

**Fix:** Ensure all CLI examples use `docker dso` syntax.

#### 4. Security Scan
**Purpose:** Detect security vulnerabilities in code

Uses: `gosec` (Go Security Scanner)
- Severity: Medium and above
- Confidence: Medium and above
- Non-blocking: Warnings only (uses `--no-fail`)

**Fix:** Address security warnings, or suppress with:
```go
// #nosec G101 -- false positive explanation
```

#### 5. DCO Check
**Purpose:** Ensure all commits have Developer Certificate of Origin sign-off

**Required for:** Pull requests only

**Requirement:** All commits must include `Signed-off-by: Name <email>` line

**Fix:**
```bash
git commit -s -m "your message"
# or amend existing commit:
git commit --amend -s
```

---

## Release Workflow (`.github/workflows/release.yml`)

### Purpose
Build and publish releases on GitHub when a version tag is pushed.

### Trigger
```bash
git tag v1.0.0
git push origin v1.0.0
```

Tags matching `v*` trigger the release workflow:
- `v1.0.0` ✓
- `v1.2.3-rc1` ✓
- `v2.0.0-alpha.1` ✓
- `release-1.0.0` ✗ (doesn't match `v*`)

### Jobs

#### Build & Release
**Steps:**

1. **Checkout:** Full git history (needed for changelog)
2. **Setup Go:** From go.mod
3. **Cache:** Go build cache and modules
4. **Verify build:** `make build`
5. **Clean artifacts:** `make clean`
6. **Run GoReleaser:** Builds binaries for multiple platforms

**What GoReleaser does:**

Uses: `.goreleaser.yml` configuration

Builds:
- `docker-dso` binary for:
  - Linux amd64, arm64
  - macOS amd64, arm64

Creates:
- Tar.gz archives with docs and license
- SHA256 checksums file
- GitHub Release with:
  - Binary downloads for each platform
  - Changelog extracted from git history
  - Installation instructions

**Failure scenarios:**
- `.goreleaser.yml` missing or invalid
- Build fails
- Go modules not tidy
- Git tag not properly formatted

**Fix:**
```bash
# Verify locally before pushing tag
make build
make clean

# Test GoReleaser locally (doesn't publish):
goreleaser release --skip-publish --rm-dist
```

---

## Configuration Files

### `.goreleaser.yml`
**Location:** Repository root

**Key sections:**
- `builds`: Define what to build (docker-dso binary)
- `archives`: Package builds into tar.gz with docs
- `checksum`: Generate SHA256 checksums
- `changelog`: Auto-generate from git history
- `release`: Configure GitHub release creation

**Template variables:**
- `{{ .Version }}` - Git tag (e.g., v1.0.0)
- `{{ .Os }}` - OS: linux, darwin
- `{{ .Arch }}` - Architecture: amd64, arm64
- `{{ .Commit }}` - Git commit SHA
- `{{ .Date }}` - Release date

### `.github/workflows/ci.yml`
**Purpose:** Continuous integration

**Key settings:**
- `go-version-file: go.mod` - Use Go version from go.mod
- `cache: true` - Cache Go modules
- `timeout-minutes: 10` - Test timeout

### `.github/workflows/release.yml`
**Purpose:** Release automation

**Key settings:**
- `tags: ["v*"]` - Trigger on version tags only
- `fetch-depth: 0` - Full history for changelog
- `GITHUB_TOKEN` - Auto-provided for GitHub API access

---

## Makefile Targets

Used by workflows:

```bash
make build      # Build docker-dso binary
make test       # Run all tests
make clean      # Remove docker-dso and temp files
make lint       # Run golangci-lint
make vet        # Run go vet
make fmt        # Format code with gofmt
make install    # Install to system (requires sudo)
make install-user # Install to user paths
```

---

## Common Issues & Fixes

### Issue: "go.mod is not tidy"
**Cause:** Dependencies added/removed but not committed

**Fix:**
```bash
go mod tidy
git add go.mod go.sum
git commit -m "chore: tidy go.mod"
```

### Issue: "docker-dso binary not found"
**Cause:** Build failed silently

**Fix:**
```bash
make build  # Check for error messages
make clean && make build  # Clean rebuild
```

### Issue: "Build cache not used"
**Cause:** go.mod or go.sum changed

**Fix:** This is normal. Cache is invalidated when dependencies change.

### Issue: "Release not triggered"
**Cause:** Tag doesn't match `v*` pattern

**Fix:**
```bash
git tag v1.0.0       # Correct
git tag release-1.0  # Doesn't match pattern
```

### Issue: "Commits missing DCO sign-off"
**Cause:** Forgot `-s` flag on commit

**Fix:**
```bash
# For new commits:
git commit -s -m "your message"

# For existing commits (in PR):
git commit --amend -s
git push origin feature-branch --force-with-lease
```

---

## Local Testing

Before pushing, test locally:

```bash
# Build and test
make build
make test
make lint

# Verify CLI guard
chmod +x scripts/validate-cli.sh
./scripts/validate-cli.sh

# Verify DCO (for commits)
git log --format='%B' | grep -i "Signed-off-by"

# Test GoReleaser config (doesn't publish)
goreleaser release --skip-publish --rm-dist
```

---

## Security Notes

- **GITHUB_TOKEN:** Automatically provided in workflows, scoped to repository
- **Secrets:** No secrets stored; uses GitHub's automatic token
- **Security scan:** Non-blocking; warnings only (doesn't fail PR)
- **Release permissions:** Only pushes to releases API with GITHUB_TOKEN

---

## Future Enhancements

Potential improvements:

1. Add code coverage reporting
2. Add automated dependency updates (Dependabot)
3. Add SLSA provenance attestation for releases
4. Add Windows binary builds
5. Add Docker image publishing
6. Add semantic versioning validation

---

## Contact & Questions

For workflow issues:
1. Check this documentation
2. Review workflow logs in GitHub Actions tab
3. Open an issue in the repository

---

**Last Updated:** May 12, 2026  
**Phase:** 1-6 Production Ready
