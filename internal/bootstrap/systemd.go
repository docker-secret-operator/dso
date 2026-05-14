package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SystemdManager manages systemd service creation and management
type SystemdManager struct {
	logger Logger
	dryRun bool
}

// NewSystemdManager creates a new systemd manager
func NewSystemdManager(logger Logger, dryRun bool) *SystemdManager {
	return &SystemdManager{
		logger: logger,
		dryRun: dryRun,
	}
}

// GenerateServiceFile generates a hardened systemd service file for DSO agent
func (sm *SystemdManager) GenerateServiceFile() string {
	return `[Unit]
Description=DSO Secret Injection Runtime Agent
Documentation=https://github.com/docker-secret-operator/dso
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root

WorkingDirectory=/var/lib/dso
StateDirectory=dso
CacheDirectory=dso
LogsDirectory=dso

ExecStart=/usr/local/bin/dso agent --config /etc/dso/dso.yaml
Restart=on-failure
RestartSec=10
StartLimitInterval=60s
StartLimitBurst=3

SyslogIdentifier=dso-agent

# Security Hardening
NoNewPrivileges=true
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ProtectClock=yes
ProtectHostname=yes
ProtectKernelLogs=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
PrivateDevices=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

# Resource Limits
LimitNOFILE=65535
MemoryLimit=500M
CPUQuota=50%

# Logging
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=multi-user.target
`
}

// InstallServiceFile writes the systemd service file
func (sm *SystemdManager) InstallServiceFile(ctx context.Context, fsOps *FilesystemOps) error {
	const servicePath = "/etc/systemd/system/dso-agent.service"

	if sm.dryRun {
		sm.logger.Info("DRY_RUN: Would install systemd service", "path", servicePath)
		return nil
	}

	// Generate service content
	serviceContent := sm.GenerateServiceFile()

	// Write service file
	if err := fsOps.SafeWriteFile(ctx, servicePath, []byte(serviceContent), 0644); err != nil {
		return ErrGroupManagement("systemd_setup", "service_file_write", err)
	}

	sm.logger.Info("Systemd service file installed", "path", servicePath)
	return nil
}

// ReloadSystemd reloads systemd daemon configuration
func (sm *SystemdManager) ReloadSystemd(ctx context.Context) error {
	if sm.dryRun {
		sm.logger.Info("DRY_RUN: Would run systemctl daemon-reload")
		return nil
	}

	cmd := exec.CommandContext(ctx, "systemctl", "daemon-reload")
	if output, err := cmd.CombinedOutput(); err != nil {
		sm.logger.Error("systemctl daemon-reload failed", "error", err.Error(), "output", string(output))
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	sm.logger.Info("Systemd daemon reloaded")
	return nil
}

// EnableService enables the DSO agent service to start at boot
func (sm *SystemdManager) EnableService(ctx context.Context) error {
	if sm.dryRun {
		sm.logger.Info("DRY_RUN: Would run systemctl enable dso-agent.service")
		return nil
	}

	cmd := exec.CommandContext(ctx, "systemctl", "enable", "dso-agent.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		sm.logger.Error("systemctl enable failed", "error", err.Error(), "output", string(output))
		return fmt.Errorf("failed to enable service: %w", err)
	}

	sm.logger.Info("DSO agent service enabled")
	return nil
}

// StartService starts the DSO agent service
func (sm *SystemdManager) StartService(ctx context.Context) error {
	if sm.dryRun {
		sm.logger.Info("DRY_RUN: Would run systemctl start dso-agent.service")
		return nil
	}

	cmd := exec.CommandContext(ctx, "systemctl", "start", "dso-agent.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		sm.logger.Error("systemctl start failed", "error", err.Error(), "output", string(output))
		return fmt.Errorf("failed to start service: %w", err)
	}

	sm.logger.Info("DSO agent service started")
	return nil
}

// StopService stops the DSO agent service
func (sm *SystemdManager) StopService(ctx context.Context) error {
	if sm.dryRun {
		sm.logger.Info("DRY_RUN: Would run systemctl stop dso-agent.service")
		return nil
	}

	cmd := exec.CommandContext(ctx, "systemctl", "stop", "dso-agent.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		// Don't fail if service isn't running
		if strings.Contains(string(output), "not loaded") || strings.Contains(string(output), "could not be found") {
			sm.logger.Warn("Service not running", "output", string(output))
			return nil
		}
		sm.logger.Error("systemctl stop failed", "error", err.Error(), "output", string(output))
		return fmt.Errorf("failed to stop service: %w", err)
	}

	sm.logger.Info("DSO agent service stopped")
	return nil
}

// DisableService disables the DSO agent service (removes from auto-start)
func (sm *SystemdManager) DisableService(ctx context.Context) error {
	if sm.dryRun {
		sm.logger.Info("DRY_RUN: Would run systemctl disable dso-agent.service")
		return nil
	}

	cmd := exec.CommandContext(ctx, "systemctl", "disable", "dso-agent.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		// Don't fail if service isn't found
		if strings.Contains(string(output), "No such file") || strings.Contains(string(output), "not found") {
			sm.logger.Warn("Service not found, skipping disable", "output", string(output))
			return nil
		}
		sm.logger.Error("systemctl disable failed", "error", err.Error(), "output", string(output))
		return fmt.Errorf("failed to disable service: %w", err)
	}

	sm.logger.Info("DSO agent service disabled")
	return nil
}

// GetServiceStatus returns the status of the DSO agent service
func (sm *SystemdManager) GetServiceStatus(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "status", "dso-agent.service")
	output, err := cmd.CombinedOutput()

	// status returns exit code 3 if service is stopped, but that's still valid output
	status := string(output)

	if err != nil && !strings.Contains(status, "Unit dso-agent.service") {
		sm.logger.Error("Failed to get service status", "error", err.Error())
		return "", fmt.Errorf("failed to get service status: %w", err)
	}

	return status, nil
}

// VerifySystemd checks if systemd is available on the system
func (sm *SystemdManager) VerifySystemd(ctx context.Context) error {
	// Check if /run/systemd/system exists
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		if os.IsNotExist(err) {
			return ErrSystemdUnavailable("systemd_verify", fmt.Errorf("systemd is not running"))
		}
		return ErrSystemdUnavailable("systemd_verify", err)
	}

	// Try to run systemctl to verify it works
	cmd := exec.CommandContext(ctx, "systemctl", "--version")
	if err := cmd.Run(); err != nil {
		return ErrSystemdUnavailable("systemd_verify", fmt.Errorf("systemctl command failed: %w", err))
	}

	sm.logger.Info("Systemd is available and functional")
	return nil
}

// GetSystemdVersion returns the systemd version
func (sm *SystemdManager) GetSystemdVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get systemd version: %w", err)
	}

	// Output format: systemd NNN (...)
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0]), nil
	}

	return "", fmt.Errorf("unexpected systemctl output format")
}

// RemoveServiceFile removes the systemd service file (for cleanup/rollback)
func (sm *SystemdManager) RemoveServiceFile(ctx context.Context, fsOps *FilesystemOps) error {
	const servicePath = "/etc/systemd/system/dso-agent.service"

	if sm.dryRun {
		sm.logger.Info("DRY_RUN: Would remove systemd service", "path", servicePath)
		return nil
	}

	// First disable and stop the service
	_ = sm.DisableService(ctx)
	_ = sm.StopService(ctx)

	// Reload systemd
	_ = sm.ReloadSystemd(ctx)

	// Remove the service file
	if err := fsOps.SafeRemove(ctx, servicePath); err != nil {
		sm.logger.Warn("Failed to remove service file, continuing", "error", err.Error())
		// Don't return error - cleanup shouldn't fail entire bootstrap
	}

	sm.logger.Info("Systemd service file removed", "path", servicePath)
	return nil
}

// GetHardeningExplanation returns explanation of systemd hardening directives
func (sm *SystemdManager) GetHardeningExplanation() string {
	return `
Systemd Security Hardening Directives
======================================

NoNewPrivileges=true
  - Process cannot gain additional privileges (e.g., via setuid binaries)

PrivateTmp=yes
  - Process gets private /tmp and /var/tmp directories
  - Prevents access to other processes' temporary files

ProtectSystem=strict
  - Most of the filesystem is mounted read-only for the service
  - Only /etc, /usr, /proc, /dev/shm are readable

ProtectHome=yes
  - Home directories become inaccessible to the service

ProtectClock=yes
  - Cannot modify system clock

ProtectHostname=yes
  - Cannot modify system hostname

ProtectKernelLogs=yes
  - Cannot read kernel logs

ProtectKernelModules=yes
  - Cannot load/unload kernel modules

ProtectControlGroups=yes
  - Cannot modify cgroup hierarchy

RestrictNamespaces=yes
  - Cannot create new namespaces

RestrictRealtime=yes
  - Cannot use real-time scheduling

RestrictSUIDSGID=yes
  - Cannot set SUID/SGID bits on files

LockPersonality=yes
  - Cannot change personality (process execution domain)

PrivateDevices=yes
  - Process has private device nodes (/dev/null, /dev/zero, etc.)

RestrictAddressFamilies
  - Only allows AF_UNIX (local sockets), AF_INET, AF_INET6 (network)
  - Prevents other socket families

Resource Limits:
  - LimitNOFILE=65535 - Max 65535 open files
  - MemoryLimit=500M   - Max 500MB memory usage
  - CPUQuota=50%       - Max 50% CPU usage

These settings provide defense-in-depth security while maintaining operational functionality.
`
}
