package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// PermissionManager handles user/group/ownership and ACL setup
type PermissionManager struct {
	logger Logger
	dryRun bool
}

// NewPermissionManager creates a new permission manager
func NewPermissionManager(logger Logger, dryRun bool) *PermissionManager {
	return &PermissionManager{
		logger: logger,
		dryRun: dryRun,
	}
}

// SetupBootstrapPermissions sets up permissions for DSO after bootstrap
// This implements the non-root cloud mode support (requirement #5-7)
func (pm *PermissionManager) SetupBootstrapPermissions(ctx context.Context, invokerUID, invokerGID int) error {
	if pm.dryRun {
		pm.logger.Info("DRY_RUN: Would setup bootstrap permissions",
			"invoker_uid", invokerUID,
			"invoker_gid", invokerGID)
		return nil
	}

	// Step 1: Create DSO group if needed
	dsoGID, err := pm.ensureDSOGroup()
	if err != nil {
		return ErrGroupManagement("permission_setup", "dso_group_creation", err)
	}
	pm.logger.Info("DSO group ready", "gid", dsoGID)

	// Step 2: Add invoking user to DSO group (if not root)
	if invokerUID != 0 {
		if err := pm.addUserToDSOGroup(invokerUID); err != nil {
			pm.logger.Warn("Could not add user to DSO group, continuing anyway",
				"uid", invokerUID, "error", err.Error())
			// Not fatal - user may still have access via group inheritance
		}
	}

	// Step 3: Setup directory permissions
	if err := pm.setupDSODirectories(dsoGID); err != nil {
		return ErrGroupManagement("permission_setup", "directory_setup", err)
	}
	pm.logger.Info("DSO directories configured")

	// Step 4: Setup file permissions
	if err := pm.setupDSOFiles(dsoGID); err != nil {
		return ErrGroupManagement("permission_setup", "file_setup", err)
	}
	pm.logger.Info("DSO files configured")

	// Step 5: Validate docker group access
	if err := pm.validateDockerGroupAccess(invokerUID); err != nil {
		pm.logger.Warn("Docker group access validation failed, continuing",
			"error", err.Error())
		// Not fatal - user may need to add docker group manually
	}

	pm.logger.Info("Bootstrap permissions setup completed successfully")
	return nil
}

// ensureDSOGroup ensures the DSO group exists, creating it if necessary
func (pm *PermissionManager) ensureDSOGroup() (int, error) {
	// Look up existing group
	dsoGroup, err := user.LookupGroup("dso")
	if err == nil {
		// Group exists
		gid, err := strconv.Atoi(dsoGroup.Gid)
		if err != nil {
			return 0, fmt.Errorf("could not parse DSO group GID: %w", err)
		}
		pm.logger.Info("DSO group already exists", "gid", gid)
		return gid, nil
	}

	// Group doesn't exist - create it via groupadd command
	// This requires root privileges
	if pm.dryRun {
		pm.logger.Info("DRY_RUN: Would create dso group")
		return 1001, nil // Return a placeholder GID in dry-run mode
	}

	pm.logger.Info("DSO group does not exist, attempting to create it")

	// Use groupadd to create the group with a specific GID
	// Try GID 1001 first, then fallback to system-assigned GID
	cmd := fmt.Sprintf("groupadd -g 1001 dso 2>/dev/null || groupadd dso")
	if err := runSystemCommand(cmd); err != nil {
		return 0, fmt.Errorf("failed to create dso group: %w", err)
	}

	// Look up the newly created group
	dsoGroup, err = user.LookupGroup("dso")
	if err != nil {
		return 0, fmt.Errorf("created dso group but failed to look it up: %w", err)
	}

	gid, err := strconv.Atoi(dsoGroup.Gid)
	if err != nil {
		return 0, fmt.Errorf("could not parse newly created DSO group GID: %w", err)
	}

	pm.logger.Info("DSO group created successfully", "gid", gid)
	return gid, nil
}

// runSystemCommand executes groupadd command safely (used for DSO group creation)
// This function is restricted to creating the "dso" group only - no arbitrary commands
func runSystemCommand(cmd string) error {
	// Safety check: only allow groupadd commands for dso group
	// Never expose user input to this function
	if cmd != "groupadd -g 1001 dso 2>/dev/null || groupadd dso" &&
		cmd != "groupadd dso" {
		return fmt.Errorf("invalid command (safety check failed)")
	}

	// Execute: groupadd -g 1001 dso (try with specific GID first)
	groupaddCmd := exec.Command("groupadd", "-g", "1001", "dso")
	if err := groupaddCmd.Run(); err != nil {
		// If GID 1001 is taken, try without specifying GID (system will assign)
		groupaddCmd = exec.Command("groupadd", "dso")
		if err := groupaddCmd.Run(); err != nil {
			return fmt.Errorf("groupadd failed: %w", err)
		}
	}

	return nil
}

// addUserToDSOGroup adds a user to the DSO group
// This typically requires root privileges
func (pm *PermissionManager) addUserToDSOGroup(uid int) error {
	u, err := user.LookupId(fmt.Sprintf("%d", uid))
	if err != nil {
		return fmt.Errorf("could not lookup user: %w", err)
	}

	// Use usermod to add user to dso group
	cmd := exec.Command("usermod", "-aG", "dso", u.Username)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add user to dso group: %w", err)
	}

	pm.logger.Info("User added to dso group", "username", u.Username, "uid", uid)
	return nil
}

// ConfigureNonRootAccess automatically sets up non-root user access to DSO
// This adds the user to dso and docker groups for command-line usage
func (pm *PermissionManager) ConfigureNonRootAccess(invokerUID int) error {
	if pm.dryRun {
		pm.logger.Info("DRY_RUN: Would configure non-root access",
			"invoker_uid", invokerUID)
		return nil
	}

	if invokerUID == 0 {
		pm.logger.Warn("ConfigureNonRootAccess called for root user, skipping")
		return nil
	}

	u, err := user.LookupId(fmt.Sprintf("%d", invokerUID))
	if err != nil {
		return fmt.Errorf("could not lookup user: %w", err)
	}

	username := u.Username

	// Add user to dso group
	cmd := exec.Command("usermod", "-aG", "dso", username)
	if err := cmd.Run(); err != nil {
		pm.logger.Warn("Could not add user to dso group",
			"username", username, "error", err.Error())
		// Not fatal - continue anyway
	} else {
		pm.logger.Info("User added to dso group", "username", username)
	}

	// Add user to docker group (if exists)
	cmd = exec.Command("usermod", "-aG", "docker", username)
	if err := cmd.Run(); err != nil {
		pm.logger.Warn("Could not add user to docker group (group may not exist)",
			"username", username, "error", err.Error())
		// Not fatal - docker group may not exist
	} else {
		pm.logger.Info("User added to docker group", "username", username)
	}

	pm.logger.Info("Non-root access configured", "username", username)
	pm.logger.Warn("User must log out and log back in for group changes to take effect",
		"username", username)

	return nil
}

// setupDSODirectories configures permissions on DSO directories
func (pm *PermissionManager) setupDSODirectories(dsoGID int) error {
	directories := []struct {
		path string
		perm os.FileMode
	}{
		{"/etc/dso", 0755},     // root:dso, readable by all (config must be accessible to CLI users)
		{"/var/lib/dso", 0770}, // root:dso, readable/writable by group
		{"/var/run/dso", 0775}, // root:dso, readable/writable by group, others can cd into
		{"/var/log/dso", 0770}, // root:dso, readable/writable by group
	}

	for _, dir := range directories {
		// Create directory if it doesn't exist
		if err := os.MkdirAll(dir.path, dir.perm); err != nil {
			// If it exists, try to change permissions only
			if !os.IsExist(err) {
				return fmt.Errorf("failed to create directory %s: %w", dir.path, err)
			}
		}

		// Set permissions
		if err := os.Chmod(dir.path, dir.perm); err != nil {
			return fmt.Errorf("failed to chmod %s: %w", dir.path, err)
		}

		// Set ownership to root:dso
		if err := os.Chown(dir.path, 0, dsoGID); err != nil {
			return fmt.Errorf("failed to chown %s: %w", dir.path, err)
		}

		pm.logger.Info("Directory configured", "path", dir.path, "perm", fmt.Sprintf("%o", dir.perm))
	}

	return nil
}

// setupDSOFiles configures permissions on DSO configuration files
func (pm *PermissionManager) setupDSOFiles(dsoGID int) error {
	files := []struct {
		path string
		perm os.FileMode
	}{
		{"/etc/dso/dso.yaml", 0640}, // rw-r-----: readable by dso group, not world-readable
	}

	for _, file := range files {
		// Only configure if file exists
		if _, err := os.Stat(file.path); err != nil {
			if os.IsNotExist(err) {
				pm.logger.Info("File does not exist yet, will be configured at creation",
					"path", file.path)
				continue
			}
			return fmt.Errorf("failed to stat %s: %w", file.path, err)
		}

		// Set permissions
		if err := os.Chmod(file.path, file.perm); err != nil {
			return fmt.Errorf("failed to chmod %s: %w", file.path, err)
		}

		// Set ownership to root:dso
		if err := os.Chown(file.path, 0, dsoGID); err != nil {
			return fmt.Errorf("failed to chown %s: %w", file.path, err)
		}

		pm.logger.Info("File configured", "path", file.path, "perm", fmt.Sprintf("%o", file.perm))
	}

	return nil
}

// validateDockerGroupAccess checks if user can access Docker
func (pm *PermissionManager) validateDockerGroupAccess(uid int) error {
	// Check if user is in docker group
	u, err := user.LookupId(fmt.Sprintf("%d", uid))
	if err != nil {
		return fmt.Errorf("could not lookup user: %w", err)
	}

	groups, err := u.GroupIds()
	if err != nil {
		return fmt.Errorf("could not get user groups: %w", err)
	}

	dockerGroup, err := user.LookupGroup("docker")
	if err != nil {
		// Docker group might not exist
		pm.logger.Warn("Docker group does not exist, skipping docker access check")
		return nil
	}

	dockerGID := dockerGroup.Gid
	hasDockerAccess := false

	for _, gid := range groups {
		if gid == dockerGID {
			hasDockerAccess = true
			break
		}
	}

	if !hasDockerAccess {
		pm.logger.Warn("User is not in docker group, may need to run: usermod -aG docker <username>",
			"user", u.Username)
	}

	return nil
}

// VerifyPermissions verifies that DSO directories have correct permissions
func (pm *PermissionManager) VerifyPermissions(dsoGID int) error {
	directories := []struct {
		path string
		perm os.FileMode
	}{
		{"/etc/dso", 0755},
		{"/var/lib/dso", 0770},
		{"/var/run/dso", 0775},
		{"/var/log/dso", 0770},
	}

	for _, dir := range directories {
		info, err := os.Stat(dir.path)
		if err != nil {
			if os.IsNotExist(err) {
				pm.logger.Warn("Directory does not exist", "path", dir.path)
				continue
			}
			return fmt.Errorf("failed to stat %s: %w", dir.path, err)
		}

		// Check if it's a directory
		if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", dir.path)
		}

		// Check ownership
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Uid != 0 {
			pm.logger.Warn("Directory owner is not root", "path", dir.path, "uid", stat.Uid)
		}
		if int(stat.Gid) != dsoGID {
			pm.logger.Warn("Directory group is not dso", "path", dir.path, "gid", stat.Gid)
		}

		// Check permissions
		actualPerm := info.Mode().Perm()
		if actualPerm != dir.perm {
			pm.logger.Warn("Directory permissions differ",
				"path", dir.path,
				"expected", fmt.Sprintf("%o", dir.perm),
				"actual", fmt.Sprintf("%o", actualPerm))
		}

		pm.logger.Info("Directory verified", "path", dir.path)
	}

	// Check config file if it exists
	configPath := "/etc/dso/dso.yaml"
	if _, err := os.Stat(configPath); err == nil {
		info, _ := os.Stat(configPath)
		stat := info.Sys().(*syscall.Stat_t)

		if stat.Uid != 0 {
			pm.logger.Warn("Config file owner is not root", "path", configPath)
		}
		if int(stat.Gid) != dsoGID {
			pm.logger.Warn("Config file group is not dso", "path", configPath)
		}

		perm := info.Mode().Perm()
		if perm != 0640 {
			pm.logger.Warn("Config file permissions incorrect",
				"path", configPath,
				"expected", "0640",
				"actual", fmt.Sprintf("%o", perm))
		}
	}

	pm.logger.Info("Permission verification completed")
	return nil
}

// GetNonRootOperationCommands returns shell commands user should run for non-root operation
func (pm *PermissionManager) GetNonRootOperationCommands(username string) []string {
	return []string{
		fmt.Sprintf("usermod -aG dso %s", username),
		fmt.Sprintf("usermod -aG docker %s", username),
		fmt.Sprintf("# User %s should log out and log back in for group changes to take effect", username),
	}
}

// DocumentPermissionModel returns documentation about the permission model
func (pm *PermissionManager) DocumentPermissionModel() string {
	return `
DSO Non-Root Permission Model
=============================

After bootstrap, the following permission structure is in place:

1. DSO Group (system group "dso")
   - All DSO system files and directories are owned by root:dso
   - Provides least-privilege access to DSO components

2. Configuration Access
   - /etc/dso/dso.yaml: 0640 (root:dso) - Owner read/write, group read
   - Users in 'dso' group can read configuration

3. State Directory
   - /var/lib/dso: 0770 (root:dso) - Owner and group can read/write
   - Users in 'dso' group can write state and logs

4. Runtime Directory
   - /var/run/dso: 0775 (root:dso) - Owner/group read/write, others can execute
   - Socket files for communication placed here

5. Log Directory
   - /var/log/dso: 0770 (root:dso) - Owner and group can read/write
   - Users in 'dso' group can read logs

To enable non-root CLI usage after bootstrap:

1. Add user to dso group:
   $ sudo usermod -aG dso <username>

2. Add user to docker group (for container operations):
   $ sudo usermod -aG docker <username>

3. Log out and log back in for group membership to take effect

After this, user can run DSO commands without sudo:
   $ docker dso status
   $ docker dso compose up
   $ docker dso logs
   $ docker dso sync

Operations requiring root will still need sudo:
   $ sudo docker dso system enable
   $ sudo docker dso system restart
`
}
