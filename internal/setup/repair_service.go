package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// RepairService handles repairs for service-related doctor checks:
//   DSO-DOCTOR-015 (unit file missing) — Moderate, requires confirmation
//   DSO-DOCTOR-016 (service not enabled) — Moderate, requires confirmation
//   DSO-DOCTOR-017 (service not active) — Moderate, requires confirmation
type RepairService struct {
	serviceName  string
	unitFilePath string
	writeFile    func(string, []byte, os.FileMode) error
	enable       func(context.Context, string) error
	start        func(context.Context, string) error
	daemonReload func(context.Context) error
}

func newRepairService() *RepairService {
	return &RepairService{
		serviceName:  "dso-agent.service",
		unitFilePath: "/etc/systemd/system/dso-agent.service",
		writeFile:    os.WriteFile,
		enable:       systemctlEnable,
		start:        systemctlStart,
		daemonReload: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "systemctl", "daemon-reload").Run()
		},
	}
}

func (rs *RepairService) planForCheck(check DoctorCheck) *RepairAction {
	switch check.ID {
	case "DSO-DOCTOR-015":
		return &RepairAction{
			ID:                   "REPAIR-SVC-001",
			IssueID:              check.ID,
			Category:             DoctorCatService,
			Description:          fmt.Sprintf("Write systemd unit file for %s at %s", rs.serviceName, rs.unitFilePath),
			RiskLevel:            RepairRiskModerate,
			RequiresConfirmation: true,
			Status:               RepairStatusPending,
		}
	case "DSO-DOCTOR-016":
		return &RepairAction{
			ID:                   "REPAIR-SVC-002",
			IssueID:              check.ID,
			Category:             DoctorCatService,
			Description:          fmt.Sprintf("Enable %s to start automatically on boot", rs.serviceName),
			RiskLevel:            RepairRiskModerate,
			RequiresConfirmation: true,
			Status:               RepairStatusPending,
		}
	case "DSO-DOCTOR-017":
		return &RepairAction{
			ID:                   "REPAIR-SVC-003",
			IssueID:              check.ID,
			Category:             DoctorCatService,
			Description:          fmt.Sprintf("Start %s", rs.serviceName),
			RiskLevel:            RepairRiskModerate,
			RequiresConfirmation: true,
			Status:               RepairStatusPending,
		}
	}
	return nil
}

// writeUnitFile writes a default dso-agent systemd unit file and reloads systemd.
func (rs *RepairService) writeUnitFile() error {
	content := []byte(dsoAgentUnitContent(rs.serviceName))
	if err := rs.writeFile(rs.unitFilePath, content, 0644); err != nil {
		return fmt.Errorf("write unit file %s: %w", rs.unitFilePath, err)
	}
	ctx := context.Background()
	if err := rs.daemonReload(ctx); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return nil
}

// enableService enables the DSO agent service for boot persistence.
func (rs *RepairService) enableService(ctx context.Context) error {
	if err := rs.enable(ctx, rs.serviceName); err != nil {
		return fmt.Errorf("systemctl enable %s: %w", rs.serviceName, err)
	}
	return nil
}

// startService starts the DSO agent service immediately.
func (rs *RepairService) startService(ctx context.Context) error {
	if err := rs.start(ctx, rs.serviceName); err != nil {
		return fmt.Errorf("systemctl start %s: %w", rs.serviceName, err)
	}
	return nil
}

func dsoAgentUnitContent(serviceName string) string {
	return fmt.Sprintf(`[Unit]
Description=DSO Agent — Docker Secret Operator runtime daemon
After=network.target docker.socket
Requires=docker.socket

[Service]
Type=simple
ExecStart=/usr/local/bin/dso-agent
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

[Install]
WantedBy=multi-user.target
`, serviceName)
}
