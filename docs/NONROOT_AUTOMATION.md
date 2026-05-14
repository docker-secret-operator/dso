# Non-Root Access Automation (v3.4.2)

Automated setup for non-root DSO CLI access in production deployments.

---

## Overview

Previously, enabling non-root users to run DSO commands required manual setup:

```bash
sudo docker dso bootstrap agent
# ... then manually:
sudo usermod -aG dso $USER
sudo usermod -aG docker $USER
# ... then logout/login
```

**Now (v3.4.2):** Use the `--enable-nonroot` flag for automatic configuration:

```bash
sudo docker dso bootstrap agent --enable-nonroot
```

---

## Usage

### Automated Setup (Recommended)

```bash
sudo docker dso bootstrap agent --enable-nonroot
```

**What it does:**
1. Creates `/etc/dso/` directory structure with proper permissions
2. Initializes DSO configuration
3. Sets up systemd service
4. **Automatically adds invoking user to `dso` and `docker` groups**
5. Outputs instructions to complete group activation

**Output:**
```
✓ DSO Agent Runtime Initialized
  Configuration: /etc/dso/dso.yaml
  Service: /etc/systemd/system/dso-agent.service
  Permissions: Configured

⚠ Important Notes:
  - User <username> configured for non-root access - log out and log back in to apply group changes
```

### Manual Setup (If Needed)

```bash
sudo docker dso bootstrap agent
# ... then manually run:
sudo usermod -aG dso $USER
sudo usermod -aG docker $USER
```

---

## Implementation Details

### Flag Definition

Added `--enable-nonroot` flag to `bootstrap agent` command:

```bash
docker dso bootstrap agent --enable-nonroot
```

### Code Changes

**1. BootstrapOptions (types.go)**
```go
type BootstrapOptions struct {
    // ...
    EnableNonRootAccess bool // Automatically configure user for non-root access
}
```

**2. CLI Flag (bootstrap.go)**
```go
cmd.Flags().BoolVar(&enableNonRootAccess, "enable-nonroot", false,
    "Automatically configure current user for non-root DSO access (agent mode only)")
```

**3. Permission Manager (permissions.go)**
```go
func (pm *PermissionManager) ConfigureNonRootAccess(invokerUID int) error {
    // - Adds user to dso group
    // - Adds user to docker group (if exists)
    // - Logs group changes
}
```

**4. Bootstrap Flow (agent.go)**
```go
// Step 9: Configure non-root access if requested
if opts.EnableNonRootAccess && currentUser.UID != 0 {
    if err := ab.perm.ConfigureNonRootAccess(currentUser.UID); err != nil {
        // Handle error
    }
}
```

---

## What Gets Automated

### User Groups

When `--enable-nonroot` is used, the invoking user is automatically added to:

- **`dso` group**: Allows reading DSO config and state files
- **`docker` group**: Allows running docker commands (if group exists)

### Equivalent Manual Commands

```bash
sudo usermod -aG dso $USER      # Added automatically
sudo usermod -aG docker $USER   # Added automatically (if group exists)
```

### File Permissions

Directory permissions are set up to allow group access:

| Path | Owner | Permissions | Allows |
|---|---|---|---|
| `/etc/dso/` | root:dso | 0755 | All users can read config |
| `/var/lib/dso/` | root:dso | 0770 | dso group can read/write state |
| `/var/log/dso/` | root:dso | 0770 | dso group can read/write logs |

---

## User Experience

### Before (v3.4.1)

```bash
# Step 1: Bootstrap
$ sudo docker dso bootstrap agent
✓ DSO Agent Runtime Initialized

# Step 2: Manual group setup
$ sudo usermod -aG dso $USER
$ sudo usermod -aG docker $USER

# Step 3: Logout and login to apply changes
$ logout
(login again)

# Step 4: Verify
$ docker dso status
```

**Issues:**
- Multi-step process
- Easy to forget group commands
- Requires logout/login cycle
- Not discoverable

### After (v3.4.2)

```bash
# Single command with automatic setup
$ sudo docker dso bootstrap agent --enable-nonroot
✓ DSO Agent Runtime Initialized
  Configuration: /etc/dso/dso.yaml
  Service: /etc/systemd/system/dso-agent.service

⚠ Important Notes:
  - User <user> configured for non-root access - log out and log back in to apply group changes

# Logout and login once
$ logout
(login again)

# Verify
$ docker dso status
```

**Improvements:**
- Single flag
- Fully automated
- Clear instructions
- Discoverable via `--help`

---

## Technical Details

### Group Addition Mechanism

Uses standard Unix `usermod` command:

```go
cmd := exec.Command("usermod", "-aG", "dso", username)
if err := cmd.Run(); err != nil {
    // Handle error
}
```

### Error Handling

- **If `dso` group not found**: Creates it automatically (already happens in bootstrap)
- **If `docker` group not found**: Logs warning, continues (not critical)
- **If `usermod` fails**: Logs error, continues (user can run manually)

### Logging

```
[INFO] User added to dso group, username=<user>
[INFO] User added to docker group, username=<user>
[WARN] User must log out and log back in for group changes to take effect, username=<user>
```

---

## When to Use

### Use `--enable-nonroot` if:

- Non-root developers need to run `docker dso` commands
- CI/CD pipelines run as non-root users
- You want automated setup without manual group commands
- You prefer single-step bootstrap

### Don't use `--enable-nonroot` if:

- Only root/administrators manage DSO
- You're using sudo for all operations
- You want to manually configure group membership
- Automation isn't needed

---

## Limitations & Notes

1. **Logout/Login Required**: Group changes don't take effect until user logs out and logs back in
   - Can use `newgrp dso` for immediate effect in current shell
   - Full logout/login recommended

2. **Root Only**: Flag requires sudo to run
   - Only root can modify group membership
   - Non-root users cannot add themselves

3. **Docker Group Optional**: If docker group doesn't exist, warning is logged but setup continues
   - Some systems may have different docker socket setup
   - Not critical for DSO operation

4. **Already in Group**: If user is already in the group, `usermod -aG` is idempotent
   - Safe to re-run bootstrap with flag

---

## Troubleshooting

### "usermod: user already in group"

This is normal and indicates the user is already in the group:

```bash
$ sudo docker dso bootstrap agent --enable-nonroot
[WARN] User already in dso group
```

Just logout/login and the permissions will be active.

### Group changes not taking effect

If you don't want to logout:

```bash
# Apply group immediately in current shell
newgrp dso
newgrp docker

# Then test
docker dso status
```

### Permission denied errors after setup

If you still get permission errors:

1. Verify user is in groups:
   ```bash
   groups $USER
   # Should show: dso docker ...
   ```

2. Verify directory permissions:
   ```bash
   ls -la /etc/dso/
   # Should show: root:dso with readable permissions
   ```

3. If still issues, manually verify:
   ```bash
   sudo usermod -aG dso $USER
   sudo usermod -aG docker $USER
   # Then logout/login
   ```

---

## Migration from v3.4.1

If you already have DSO running with manual setup:

### Option 1: Leave as-is
Your current setup works fine. No changes needed.

### Option 2: Re-bootstrap with automation
```bash
# Just re-run with --enable-nonroot
sudo docker dso bootstrap agent --enable-nonroot
```

This is safe—it will:
- Update any changed permissions
- Re-add users to groups (idempotent)
- Keep existing configuration intact

---

## Future Enhancements

Potential improvements for future versions:

1. **Interactive prompt**: Ask during bootstrap if non-root setup is wanted
2. **Immediate activation**: Use process privilege escalation to apply groups without logout
3. **Per-user configuration**: Fine-grained access control for multiple users
4. **Group creation**: Create custom groups for team-based access

---

## See Also

- [SETUP_GUIDE.md](SETUP_GUIDE.md) — Complete setup guide
- [QUICKREF.md](QUICKREF.md) — Quick command reference
- [DOCUMENTATION_INDEX.md](../DOCUMENTATION_INDEX.md) — Documentation navigation
