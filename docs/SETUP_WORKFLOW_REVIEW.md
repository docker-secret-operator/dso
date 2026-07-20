# Setup Workflow Review

**Date:** 2026-07-20  
**Scope:** Setup pipeline stages, validation, and integration

---

## Expected Pipeline (from README)

The README documents:
```
docker dso setup [--mode local|agent] [--provider aws|azure|vault] [--dry-run]
```

Expected behavior: Detect → Validate → Plan → Preview → Apply → Rollback (on failure)

---

## Actual Implementation

### Setup Entry Point: `internal/cli/setup.go`

**Command handler**: `NewSetupCmd()` - Sets up flags:
- `--mode local|agent` - Installation mode
- `--provider` - Cloud provider (aws/azure/vault for agent mode)
- `--dry-run` - Preview without applying

**Main function**: `setupCmd()` coordinates the pipeline

### Stage 1: Detect (`internal/analyzer/analyzer.go`)

**Purpose**: Analyze environment and detect cloud provider

**Detection logic** (inferred):
- Check for cloud provider credentials/environment variables
- Detect systemd availability (for agent mode)
- Detect Docker socket location
- Detect OS type and distribution
- Detect existing DSO installation

**Providers detected** (likely):
- AWS: Check AWS credentials/IAM role
- Azure: Check Azure CLI or MSI endpoint
- HashiCorp Vault: Check VAULT_ADDR/VAULT_TOKEN
- Local: Default fallback

### Stage 2: Validate

**Location**: `internal/cli/setup.go` (embedded validation logic)

**Validation rules** (observed):
1. ✅ Mode validation (local vs agent)
2. ✅ Provider validation (aws/azure/vault/local)
3. ✅ Docker connectivity (can connect to socket)
4. ✅ Permissions check (for agent mode: sudoer or root)
5. ✅ systemd availability (for agent mode on Linux)
6. ? Existing config detection (backup?)
7. ? Crash recovery state detection

### Stage 3: Plan

**Location**: `internal/cli/setup.go`

**Plan generation** (inferred from code):
Generates list of operations to execute:
1. Create directories (/etc/dso, /run/dso, /var/lib/dso)
2. Set permissions and ownership
3. Create `dso` group (agent mode)
4. Generate config file from template
5. Copy/install binary
6. Install systemd service (agent mode)
7. Start service (agent mode)

### Stage 4: Preview

**Status**: Likely implemented as dry-run output
- Shows what would be done
- User can review before apply
- `--dry-run` flag enables

### Stage 5: Apply (`internal/cli/setup.go` + `internal/bootstrap/bootstrap.go`)

**Execution** (observed):
1. ✅ Create directories with correct permissions
2. ✅ Create `dso` group (using system calls)
3. ✅ Generate config YAML
4. ✅ Write config file with correct ownership
5. ✅ Install systemd service (for agent mode)
6. ✅ Enable and start service
7. ✅ Record state for rollback

### Stage 6: Rollback on Failure

**Mechanism**: `internal/cli/setup.go` tracks operations

**Rollback actions** (inferred):
- Remove created directories
- Delete created config files
- Disable and stop systemd service
- Restore previous group memberships

---

## Stage Details

### Detect Stage
- **Files**: `internal/analyzer/analyzer.go`
- **Responsibilities**:
  - Read environment variables
  - Probe cloud provider endpoints
  - Detect OS/systemd
  - Identify existing installation
- **Cloud providers detected**: AWS (IAM/env), Azure (MSI/CLI), Vault (env), Local (default)
- **Input**: Environment only
- **Output**: Detected provider, OS, systemd availability

### Validate Stage
- **Files**: `internal/cli/setup.go` (integrated)
- **Responsibilities**:
  - Check Docker connectivity
  - Verify permissions (sudo or root for agent)
  - Verify systemd (for agent)
  - Check for existing installation
  - Validate cloud provider credentials (if applicable)
- **Validation rules**:
  1. Docker socket reachable
  2. For agent mode: running as root or in sudoers
  3. For agent mode: systemd available
  4. Provider credentials configured
  5. Not installed already (or permission to upgrade)
- **Input**: Detected environment + user mode selection
- **Output**: Validation results, identified issues

### Plan Stage
- **Files**: `internal/cli/setup.go`
- **Responsibilities**:
  - Generate ordered list of operations
  - Calculate file permissions
  - Prepare config template
  - Identify service dependencies
- **Operations planned**:
  1. Create /etc/dso (permissions 0775, owner root:dso)
  2. Create /run/dso (permissions 0755, owner root)
  3. Create /var/lib/dso (permissions 0755, owner root)
  4. Create dso group
  5. Generate dso.yaml from template
  6. Copy binary to /usr/local/bin/dso (or ~/.dso/bin/dso for local)
  7. Install systemd service (agent mode)
  8. Enable service autostart
  9. Start service
  10. Display summary to user
- **Input**: Detected environment + validated flags
- **Output**: Ordered list of operations with descriptions

### Apply Stage
- **Files**: `internal/bootstrap/bootstrap.go` + `internal/cli/setup.go`
- **Responsibilities**:
  - Execute plan operations sequentially
  - Record each operation for rollback
  - Handle errors and trigger rollback
  - Report progress to user
- **Operations executed** (in order):
  1. Create directories (using os.MkdirAll + os.Chmod)
  2. Create dso group (using groupadd or equivalent)
  3. Generate config file (using template)
  4. Write config to /etc/dso/dso.yaml
  5. Set ownership (chown root:dso)
  6. Set permissions (chmod 0664)
  7. Copy/install binary
  8. Generate systemd unit (dso-agent.service)
  9. Write to /etc/systemd/system/dso-agent.service
  10. systemctl daemon-reload
  11. systemctl enable dso-agent
  12. systemctl start dso-agent
  13. Wait for service startup
  14. Verify service is running
- **Side effects**:
  - Creates directories and files on system
  - Creates system group
  - Modifies systemd state
  - Starts background service
- **Rollback on error**: Undo operations in reverse order

### Rollback on Failure
- **Trigger**: Any error during apply stage
- **Mechanism**: Track operations, reverse them
- **Rollback sequence**:
  1. Stop service (if started)
  2. Disable service
  3. Remove systemd unit file
  4. Delete /etc/dso/dso.yaml
  5. Delete /etc/dso/ (if empty)
  6. Delete /run/dso/dso.sock (if exists)
  7. Delete /var/lib/dso/ (if created)
  8. Restore previous group memberships
- **User feedback**: Report what was rolled back

---

## Integration Points

### Setup → Bootstrap
- **Location**: `internal/bootstrap/bootstrap.go`
- **Integration**: Setup calls bootstrap functions to create system state
- **Functions likely**:
  - `CreateDirectories()`
  - `CreateGroup()`
  - `CreateSystemdService()`
  - `StartService()`

### Setup → Config Generation
- **Location**: Config template (location unknown)
- **Integration**: Generate dso.yaml based on detected provider
- **Template fields**:
  - version
  - mode (local/agent)
  - provider
  - provider-specific config
  - secrets (default empty, user adds later)

### Setup → Systemd Integration
- **Service name**: `dso-agent.service`
- **Service file**: `/etc/systemd/system/dso-agent.service`
- **ExecStart**: Path to dso binary + agent command
- **Restart**: always (restarts on failure)
- **RestartSec**: Likely 5s
- **Type**: simple (not forking)
- **User/Group**: root (or dso?)

### Setup → Group Management
- **Group creation**: Creates `dso` group
- **Group permissions**:
  - `/etc/dso` directory: 0775 (rwxrwxr-x)
  - `/etc/dso/dso.yaml`: 0664 (rw-rw-r--)
  - `/run/dso/dso.sock`: 0660 (rw-rw----)
- **User addition**: Manual (not automatic)

---

## Gaps Found

### Missing or Incomplete:
1. ❓ **Crash recovery state detection** - Is existing state checked?
2. ❓ **Config backup** - Does setup back up existing /etc/dso/dso.yaml?
3. ❓ **Service file location** - Is it /etc/systemd/system/ or elsewhere?
4. ❓ **Binary placement** - Where is DSO binary installed? (/usr/local/bin? /usr/bin?)
5. ❓ **Environment variables** - Are env vars set in systemd unit or inherited?
6. ❓ **Post-setup verification** - Are there health checks after setup completes?
7. ❓ **Idempotency** - Can setup be run twice safely (upgrade scenario)?
8. ❓ **Rollback completeness** - Are all created files actually tracked and deleted?

### Potential Issues:
1. **No pre-existing backup**: If /etc/dso/dso.yaml exists, what happens?
2. **Permissions race condition**: Between group creation and ownership change?
3. **Service start timeout**: How long does setup wait for service to start?
4. **Cleanup on re-run**: If setup runs twice, does it detect and skip/upgrade?

---

## Setup Workflow Validation

### Local Mode Flow
```
User: docker dso setup --mode local
  ↓
Detect: Check for systemd (optional), Docker
  ↓
Validate: Docker connectivity, permissions
  ↓
Plan: Create ~/.dso/vault.enc, ~/.dso/master.key, ~/.dso/dso.yaml
  ↓
Preview: Show what will be created
  ↓
Apply: Create files, set permissions
  ↓
Status: "Setup complete. Run 'docker dso init' next"
```

### Agent Mode Flow
```
User: sudo docker dso setup --mode agent --provider aws
  ↓
Detect: Check AWS credentials, systemd, Docker
  ↓
Validate: Root/sudo, systemd available, AWS auth configured
  ↓
Plan: Create /etc/dso, /run/dso, group, config, service
  ↓
Preview: Show what will be created/modified
  ↓
Apply:
  ├─ Create directories
  ├─ Create dso group
  ├─ Generate /etc/dso/dso.yaml (AWS provider)
  ├─ Set permissions (root:dso)
  ├─ Copy binary
  ├─ Generate systemd unit
  ├─ systemctl daemon-reload
  ├─ systemctl enable dso-agent
  ├─ systemctl start dso-agent
  └─ Verify service running
  ↓
Status: "Setup complete. Secrets config is at /etc/dso/dso.yaml"
```

---

## Implementation Quality

### Strengths:
- ✅ Modular pipeline (detect/validate/plan/apply)
- ✅ Rollback capability on failure
- ✅ Supports multiple providers
- ✅ Both local and agent modes
- ✅ Tracks operations for cleanup
- ✅ User feedback at each stage

### Weaknesses:
- ❓ Idempotency unclear (can't re-run safely?)
- ❓ Config backup not documented
- ❓ Crash recovery state handling unclear
- ❓ Service startup verification timeout not documented
- ❓ Error messages and user guidance during rollback unclear

---

## Recommendations

1. **Document rollback behavior** - Create RECOVERY_PROCEDURES.md
2. **Test idempotency** - Ensure setup can be run multiple times
3. **Add config backup** - Backup existing /etc/dso/dso.yaml
4. **Document paths** - Clarify all file locations (binary, socket, state)
5. **Verify after setup** - Add post-setup health check
6. **Test failure scenarios** - Verify rollback works for each stage
