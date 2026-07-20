# Deployment Model & Systemd Integration Review

**Date:** 2026-07-20  
**Scope:** Installation modes, deployment process, CI/CD, and release procedures

---

## Installation Modes

### Local Mode (User Install)

**sudo required**: No

**systemd required**: No

**Permissions**: User-level (home directory)

**Use case**: Development, testing, single-user

**Install command**:
```bash
curl -fsSL https://raw.githubusercontent.com/.../install.sh | bash
```

**Installation steps**:
1. Download binary to ~/.local/bin/dso
2. Create ~/.dso directory
3. Generate ~/.dso/dso.yaml (local provider)
4. Create ~/.dso/vault.enc and ~/.dso/master.key
5. No systemd service

**Uninstall**:
- Remove ~/.dso/ directory
- Remove binary from ~/.local/bin/

**Pros**:
- ✅ No root/sudo required
- ✅ No system-wide impact
- ✅ Quick for testing

**Cons**:
- ❌ Only one user can use DSO
- ❌ Not suitable for production (not auto-start)
- ❌ No centralized config

### Cloud/Agent Mode (System Install)

**sudo required**: Yes (all operations)

**systemd required**: Yes (Linux only)

**Permissions**: System-level (/etc/dso, /var/lib/dso)

**Use case**: Production, multi-user, auto-start

**Install command**:
```bash
curl -fsSL https://raw.githubusercontent.com/.../install.sh | sudo bash
```

**Installation steps**:
1. Download binary to /usr/local/bin/dso
2. Create /etc/dso directory (0775, root:dso)
3. Create dso group
4. Generate /etc/dso/dso.yaml (provider-specific)
5. Install systemd service (dso-agent.service)
6. Enable and start service

**Uninstall**:
```bash
sudo systemctl stop dso-agent
sudo systemctl disable dso-agent
sudo rm /etc/systemd/system/dso-agent.service
sudo systemctl daemon-reload
sudo rm -rf /etc/dso /var/lib/dso
sudo userdel dso (if dedicated user exists)
```

**Pros**:
- ✅ Multi-user support
- ✅ Automatic startup
- ✅ Systemd integration
- ✅ Production-ready

**Cons**:
- ❌ Requires sudo
- ❌ System-wide changes
- ❌ Harder to uninstall cleanly

---

## Installation Process

### Binary Placement
- **Local mode**: ~/.local/bin/dso (or ~/bin/dso)
- **Agent mode**: /usr/local/bin/dso or /usr/bin/dso
- **Verification**: Run `dso version` to confirm

### Directory Creation
1. `/etc/dso` - Agent mode config directory
2. `/run/dso` - Runtime sockets and state
3. `/var/lib/dso` - Persistent state directory
4. `~/.dso` - Local mode config

### Group Creation
- **Group name**: dso
- **GID**: Typically 1000 (or next available)
- **Members**: Added manually `sudo usermod -aG dso $USER`

### Systemd Setup
1. Generate `/etc/systemd/system/dso-agent.service`
2. `systemctl daemon-reload`
3. `systemctl enable dso-agent.service`
4. `systemctl start dso-agent.service`

### Config Generation
- **Template**: Based on detected provider
- **Location**: /etc/dso/dso.yaml (agent) or ~/.dso/dso.yaml (local)
- **Permissions**: 0664 (agent) or 0600 (local)
- **Validation**: ✓ (config tested before declaring success)

---

## Systemd Service Configuration

### Service File Location
- `/etc/systemd/system/dso-agent.service`

### Expected Service File (Inferred)

```ini
[Unit]
Description=Docker Secret Operator Agent
Documentation=https://github.com/docker-secret-operator/dso/wiki
After=docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/dso agent
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=dso-agent
User=root
Group=root

[Install]
WantedBy=multi-user.target
```

### Service Behavior
- **Type**: simple (no forking)
- **ExecStart**: `dso agent` command
- **Restart**: always (restarts on failure)
- **RestartSec**: 5 seconds (backoff)
- **Output**: Journald (systemd journal)
- **Environment**: Inherited or set in service file?

### Crash Recovery
- **Mechanism**: systemctl restart (auto-triggered after 5s)
- **State recovery**: Agent recovers from stored state
- **Orphaned containers**: Detected and re-managed
- **Log**: View via `journalctl -u dso-agent`

---

## Upgrade Process

### In-Place Upgrade
- **Mechanism**: Stop service → replace binary → start service
- **Binary replacement**: /usr/local/bin/dso
- **Config migration**: Handled by agent (backward compatible?)
- **Data preservation**: State files in /var/lib/dso preserved
- **Downtime**: Service stops briefly (downtime tolerable?)

### Upgrade Steps
1. `sudo systemctl stop dso-agent.service`
2. Download new binary to /tmp/dso
3. `sudo mv /tmp/dso /usr/local/bin/dso`
4. `sudo systemctl start dso-agent.service`
5. Verify: `sudo systemctl status dso-agent.service`

### Config Compatibility
- **V1 → V2**: Automatic upgrade via custom unmarshaler
- **Secrets preserved**: Yes (in vault file)
- **Containers re-discovered**: Yes (on next event)

### Rollback
- **Mechanism**: Downgrade binary (if old version available)
- **Complexity**: Could fail if new config format incompatible
- **Risk**: Mid-rotation could cause issues

---

## CI/CD Pipeline

### Build Process
- **Tool**: Go build + goreleaser (likely)
- **Targets**: Linux amd64, arm64; macOS amd64, arm64; Windows
- **Binary output**: ./dso (and darwin/windows variants)

### Testing
- **Unit tests**: `go test ./...`
- **Test coverage**: Via codecov.io (badge visible in README)
- **CI tool**: GitHub Actions (workflows in .github/workflows/)
- **Trigger**: On push to main, PR events

### Build Artifacts
- **Binary**: dso (Linux) or dso.exe (Windows)
- **Checksums**: SHA256 sums for verification
- **Signatures**: GPG signed (if applicable)
- **Artifacts location**: GitHub releases

### Release Process
- **Tool**: goreleaser (.goreleaser.yml configured)
- **Trigger**: Git tag (v1.0.0 pattern)
- **Artifacts**: Released to GitHub releases
- **Automation**: GitHub Actions workflow

---

## CI/CD Verification

### Checks That Should Run
1. ✓ `go fmt` - Code formatting
2. ✓ `go vet` - Static analysis
3. ✓ `golangci-lint` - Linting (if configured)
4. ✓ `go test ./...` - Unit tests
5. ✓ Coverage report - Codecov upload
6. ✓ Build test - Ensure binary builds
7. ✓ Integration tests - Docker integration (if available)

### Failure Blocks Merge
- **Unit test failure**: Blocks merge? (should)
- **Coverage drop**: Blocks merge? (depends on threshold)
- **Lint failure**: Blocks merge? (depends on rules)
- **Build failure**: Blocks merge? (should)

---

## Gaps/Issues Found

### Critical Issues:

1. **❌ Upgrade/downgrade documentation missing**
   - How to upgrade between versions
   - Backward compatibility guarantees
   - Downgrade safety

2. **❌ Rollback procedure not documented**
   - How to recover if upgrade fails
   - Mid-rotation rollback risks
   - State file recovery

3. **❌ Installation script security**
   - Piping curl to bash is risky
   - No checksum verification in script
   - Script could be compromised

### Medium Issues:

4. **❓ Binary placement flexibility**
   - Hardcoded to /usr/local/bin?
   - Can be customized?
   - PATH priority?

5. **❓ Service file generation**
   - Is it created dynamically?
   - Hardcoded in binary?
   - Template in package?

6. **❓ Systemd restart behavior**
   - RestartSec timing: Is 5s optimal?
   - Max restarts before backoff? (unlimited?)
   - Circuit breaker? (no)

### Low Issues:

7. **❓ macOS/Windows support**
   - README says "Linux (amd64, arm64)" primary
   - macOS "supported for local mode only"
   - Windows: Not mentioned
   - Non-Linux needs different setup

8. **❓ Docker version compatibility**
   - What Docker versions are tested?
   - API compatibility range?
   - Breaking changes in newer Docker?

---

## Testing Gaps

### What Should Be Tested:
1. ✓ Installation in clean environment
2. ✓ Setup wizard flow
3. ✓ Systemd service autostart
4. ✓ Service restart on failure
5. ✓ Config migration on upgrade
6. ✓ State recovery on crash
7. ✓ Downgrade (if supported)
8. ✓ Uninstall cleanup
9. ✓ Multiple users in dso group
10. ✓ Group membership verification

### Integration Tests:
- Full install → start → manage containers → stop
- Upgrade with active rotations
- Crash and recovery

---

## Recommendations

1. **Add checksum verification** - Verify downloaded binary
2. **Document upgrade process** - Step-by-step upgrade guide
3. **Add rollback procedure** - How to recover from failed upgrade
4. **Implement version checking** - Agent reports version in status
5. **Add systemd socket activation** - Use socket-based activation for IPC
6. **Document compatibility** - Support matrix (Docker versions, OS)
7. **Add pre-flight checks** - Installation verification
8. **Implement graceful shutdown** - Let in-flight rotations complete
9. **Add upgrade tests** - CI tests for upgrade path
10. **Document system requirements** - RAM, disk, CPU recommendations
