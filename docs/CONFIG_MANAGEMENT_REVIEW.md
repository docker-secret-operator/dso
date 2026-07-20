# Configuration File Management & Permissions Review

**Date:** 2026-07-20  
**Scope:** Directory creation, file generation, permissions, and systemd integration

---

## Directory Structure & Permissions

### Expected Layout

| Path | Permissions | Owner | Purpose | Notes |
|------|-----------|-------|---------|-------|
| /etc/dso/ | 0775 | root:dso | Config directory | DSO group can read/execute |
| /etc/dso/dso.yaml | 0664 | root:dso | Main config | DSO group can read |
| /run/dso/ | 0755 | root | Runtime directory | Temp sockets |
| /run/dso/dso.sock | 0660 | root:dso | IPC socket | DSO group can access |
| /var/lib/dso/ | 0755 | root | State directory | Crash recovery, lock files |
| ~/.dso/ | 0700 | user | Local mode home | User-only for local mode |
| ~/.dso/master.key | 0600 | user | Master key | Plaintext key (security risk) |
| ~/.dso/vault.enc | 0600 | user | Vault file | Encrypted secrets |

---

## File Creation Process

### Directory Creation (`bootstrap.go`)

**Order of operations**:
1. Create /etc/dso with permissions 0775
2. Create /run/dso with permissions 0755
3. Create /var/lib/dso with permissions 0755
4. Set ownership (root:dso for /etc/dso, root for others)

**Error handling**:
- Skip if exists and permissions correct
- Error if exists with wrong permissions
- Create parent dirs recursively

### Config File Generation

**Process**:
1. Create YAML template based on detected provider
2. Fill in provider-specific settings
3. Generate dso.yaml
4. Write to /etc/dso/dso.yaml or ~/.dso/dso.yaml
5. Set permissions (0664 or 0600 for local)

**Example for AWS**:
```yaml
version: v1.0.0
mode: agent
providers:
  aws:
    type: aws
    region: us-east-1
    auth:
      method: iam_role
    retry:
      attempts: 3
      backoff: 1s

agent:
  cache: true
  watch:
    mode: polling
    polling_interval: 30s

secrets: []
```

**Backup of existing**:
- ❓ Does setup back up existing config?
- ❓ If exists, error or overwrite?
- ❓ Is old config preserved?

### Systemd Unit File Creation

**File**: `/etc/systemd/system/dso-agent.service`

**Example content** (inferred):
```ini
[Unit]
Description=Docker Secret Operator Agent
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

[Install]
WantedBy=multi-user.target
```

**Integration**:
1. Generate service file
2. Write to /etc/systemd/system/
3. systemctl daemon-reload
4. systemctl enable dso-agent.service
5. systemctl start dso-agent.service

---

## Group Management

### DSO Group Creation

**Process**:
1. Check if `dso` group exists
2. If not, create via `groupadd dso`
3. Set GID to specific value (1000? configurable?)

**User Addition**:
- ❓ Automatic or manual?
- ❓ Documentation says: `sudo usermod -aG dso $USER` (manual)
- ❓ Setup should document this step

### Group Membership Verification

**After setup**:
- User should be in `dso` group
- Can be verified: `id` or `groups` command
- Must log out/in for group change to apply (or use `newgrp`)

---

## Config File Format & Validation

### Format Support
- **YAML** (primary)
- **No JSON support** (assumed)
- **No TOML support** (assumed)

### Validation After Creation
- ✅ Parse YAML (detect syntax errors)
- ✅ Validate required fields
- ✅ Validate provider config
- ✅ Verify provider connectivity (optional?)

### Backup Strategy
- **Current**: Unknown if backup exists
- **Recommendation**: Backup to dso.yaml.bak
- **Rollback**: If new config invalid, restore from backup

---

## systemd Integration

### Service File Details

**Name**: `dso-agent.service`

**Key settings** (inferred from README):
- ExecStart: Full path to dso binary + `agent` command
- Restart: always (restarts on failure)
- RestartSec: Likely 5 seconds
- Type: simple (not forking)
- StandardOutput: journal (sends to systemd journal)
- StandardError: journal
- After: docker.service (requires Docker running)
- WantedBy: multi-user.target

**Environment variables**:
- ❓ How is config file path specified?
- ❓ How is log level specified?
- ❓ Are env vars in service file or /etc/dso/dso.env?

### Service Lifecycle

**Enable**:
```bash
systemctl enable dso-agent.service
# Creates symlink in /etc/systemd/system/multi-user.target.wants/
```

**Start**:
```bash
systemctl start dso-agent.service
# Starts service immediately
```

**Status**:
```bash
systemctl status dso-agent.service
# Shows running state, last message, uptime
```

**Logs**:
```bash
journalctl -u dso-agent
# View all logs for service
```

### Autostart on Boot
- **Enabled**: systemctl enable makes it auto-start
- **Persistent**: Symlink survives reboots
- **Dependency**: Starts after docker.service
- **Failure handling**: Restarts automatically

---

## Gaps/Issues Found

### Critical Issues:

1. **❌ Config backup not documented**
   - If /etc/dso/dso.yaml exists, what happens?
   - Overwrite? Error? Backup?
   - Risk: Existing production config lost

2. **❌ Master key stored as plaintext**
   - ~/.dso/master.key is plaintext (security risk)
   - If home directory world-readable, key exposed
   - No encryption at rest
   - No key rotation mechanism

3. **❌ Permissions race condition**
   - Between group creation and ownership change
   - Group created → ownership set to root:dso
   - If user added to group in between: permission errors

### Medium Issues:

4. **❓ Config validation after creation**
   - Is newly created config tested?
   - Does setup verify it can be loaded?
   - What if provider auth fails during validation?

5. **❓ Service restart behavior**
   - RestartSec value unknown (assumed 5s)
   - Max retries before giving up? (unlimited?)
   - Log output on restart? (yes via journald)

6. **❓ Environment variable handling**
   - How is config path passed to agent?
   - Via service file ExecStart? (probably)
   - Via environment file /etc/dso/dso.env? (maybe)
   - Via default search path? (likely)

### Low Issues:

7. **❓ Permissions for state directory**
   - /var/lib/dso created as 0755 (world-readable)
   - If state contains sensitive info, risk
   - Should be 0700 (root only)?

8. **❓ Systemd journal integration**
   - Is dso binary logging to stdout/stderr?
   - Or directly to syslog?
   - Log rotation handled by journald? (yes)

---

## Testing Gaps

### What Should Be Tested:
1. ✓ Directory creation with correct permissions
2. ✓ Group creation (if doesn't exist)
3. ✓ Config file generation and validation
4. ✓ Systemd service file creation
5. ✓ systemctl enable/start success
6. ✓ Service autostart on boot (requires reboot)
7. ✓ Log output to journald
8. ✓ Graceful shutdown (systemctl stop)
9. ✓ Crash recovery (systemctl restart)
10. ✓ Config reload (if supported)

### Integration Tests:
- Full setup → service startup → log verification
- Service restart recovery
- Permission validation after setup

---

## Recommendations

1. **Add config backup** - Backup existing config before overwrite
2. **Implement config validation** - Test loaded config before declaring success
3. **Secure master key** - Use /run/dso/master.key (tmpfs) for local mode
4. **Document service file** - Show complete dso-agent.service
5. **Add key rotation** - Mechanism to rotate master key
6. **Verify group membership** - Check `groups` command after adding user
7. **Document environment setup** - Show all environment variables
8. **Add permission check command** - `dso doctor` should verify permissions
9. **Test systemd restart** - Verify service restarts on failure
10. **Document log locations** - Show where to find logs (journalctl)
