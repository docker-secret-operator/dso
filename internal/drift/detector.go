package drift

import (
	"fmt"
	"time"
)

// SecretDriftDetector detects unexpected secret changes
type SecretDriftDetector struct {
	getSecrets func() (map[string]interface{}, error)
	lastState  map[string]interface{}
}

// NewSecretDriftDetector creates a new secret drift detector
func NewSecretDriftDetector(getSecrets func() (map[string]interface{}, error)) *SecretDriftDetector {
	return &SecretDriftDetector{
		getSecrets: getSecrets,
		lastState:  make(map[string]interface{}),
	}
}

func (s *SecretDriftDetector) ID() string       { return "detector_secret_drift" }
func (s *SecretDriftDetector) Name() string     { return "Secret Drift Detector" }
func (s *SecretDriftDetector) Type() DriftType { return DriftSecret }

func (s *SecretDriftDetector) Detect(ctx interface{}) ([]DriftFinding, error) {
	secrets, err := s.getSecrets()
	if err != nil {
		return nil, err
	}

	var findings []DriftFinding
	for key, value := range secrets {
		if lastValue, exists := s.lastState[key]; exists {
			if lastValue != value {
				findings = append(findings, DriftFinding{
					ID:          fmt.Sprintf("drift_secret_%d", time.Now().UnixNano()),
					Type:        DriftSecret,
					Severity:    SeverityCritical,
					Status:      StatusDetected,
					Resource:    key,
					Description: fmt.Sprintf("Secret %s has changed unexpectedly", key),
					DetectedAt:  time.Now(),
				})
			}
		}
	}

	s.lastState = secrets
	return findings, nil
}

// PolicyDriftDetector detects policy modifications
type PolicyDriftDetector struct {
	getPolicies func() (map[string]interface{}, error)
	lastState   map[string]interface{}
}

// NewPolicyDriftDetector creates a new policy drift detector
func NewPolicyDriftDetector(getPolicies func() (map[string]interface{}, error)) *PolicyDriftDetector {
	return &PolicyDriftDetector{
		getPolicies: getPolicies,
		lastState:   make(map[string]interface{}),
	}
}

func (p *PolicyDriftDetector) ID() string       { return "detector_policy_drift" }
func (p *PolicyDriftDetector) Name() string     { return "Policy Drift Detector" }
func (p *PolicyDriftDetector) Type() DriftType { return DriftPolicy }

func (p *PolicyDriftDetector) Detect(ctx interface{}) ([]DriftFinding, error) {
	policies, err := p.getPolicies()
	if err != nil {
		return nil, err
	}

	var findings []DriftFinding
	for id := range policies {
		if _, exists := p.lastState[id]; !exists {
			findings = append(findings, DriftFinding{
				ID:          fmt.Sprintf("drift_policy_%d", time.Now().UnixNano()),
				Type:        DriftPolicy,
				Severity:    SeverityHigh,
				Status:      StatusDetected,
				Resource:    id,
				Description: fmt.Sprintf("Policy %s was modified", id),
				DetectedAt:  time.Now(),
			})
		}
	}

	p.lastState = policies
	return findings, nil
}

// UserDriftDetector detects user and role changes
type UserDriftDetector struct {
	getUsers func() (map[string]interface{}, error)
	lastState map[string]interface{}
}

// NewUserDriftDetector creates a new user drift detector
func NewUserDriftDetector(getUsers func() (map[string]interface{}, error)) *UserDriftDetector {
	return &UserDriftDetector{
		getUsers: getUsers,
		lastState: make(map[string]interface{}),
	}
}

func (u *UserDriftDetector) ID() string       { return "detector_user_drift" }
func (u *UserDriftDetector) Name() string     { return "User Drift Detector" }
func (u *UserDriftDetector) Type() DriftType { return DriftUser }

func (u *UserDriftDetector) Detect(ctx interface{}) ([]DriftFinding, error) {
	users, err := u.getUsers()
	if err != nil {
		return nil, err
	}

	var findings []DriftFinding
	for userID := range users {
		if _, exists := u.lastState[userID]; !exists {
			findings = append(findings, DriftFinding{
				ID:          fmt.Sprintf("drift_user_%d", time.Now().UnixNano()),
				Type:        DriftUser,
				Severity:    SeverityHigh,
				Status:      StatusDetected,
				Resource:    userID,
				Description: fmt.Sprintf("User permissions changed for %s", userID),
				DetectedAt:  time.Now(),
			})
		}
	}

	u.lastState = users
	return findings, nil
}

// BackupDriftDetector detects missing or stale backups
type BackupDriftDetector struct {
	getBackupAge func() (int, error) // Returns days since last backup
}

// NewBackupDriftDetector creates a new backup drift detector
func NewBackupDriftDetector(getBackupAge func() (int, error)) *BackupDriftDetector {
	return &BackupDriftDetector{
		getBackupAge: getBackupAge,
	}
}

func (b *BackupDriftDetector) ID() string       { return "detector_backup_drift" }
func (b *BackupDriftDetector) Name() string     { return "Backup Drift Detector" }
func (b *BackupDriftDetector) Type() DriftType { return DriftBackup }

func (b *BackupDriftDetector) Detect(ctx interface{}) ([]DriftFinding, error) {
	days, err := b.getBackupAge()
	if err != nil {
		return nil, err
	}

	var findings []DriftFinding
	severity := SeverityInfo
	if days > 7 {
		severity = SeverityLow
	}
	if days > 14 {
		severity = SeverityMedium
	}
	if days > 30 {
		severity = SeverityHigh
	}
	if days > 60 {
		severity = SeverityCritical
	}

	if days > 1 {
		findings = append(findings, DriftFinding{
			ID:          fmt.Sprintf("drift_backup_%d", time.Now().UnixNano()),
			Type:        DriftBackup,
			Severity:    severity,
			Status:      StatusDetected,
			Resource:    "backup",
			Description: fmt.Sprintf("Last backup is %d days old", days),
			DetectedAt:  time.Now(),
		})
	}

	return findings, nil
}

// PluginDriftDetector detects plugin enable/disable changes
type PluginDriftDetector struct {
	getPlugins func() (map[string]bool, error) // plugin ID -> enabled
	lastState  map[string]bool
}

// NewPluginDriftDetector creates a new plugin drift detector
func NewPluginDriftDetector(getPlugins func() (map[string]bool, error)) *PluginDriftDetector {
	return &PluginDriftDetector{
		getPlugins: getPlugins,
		lastState:  make(map[string]bool),
	}
}

func (p *PluginDriftDetector) ID() string       { return "detector_plugin_drift" }
func (p *PluginDriftDetector) Name() string     { return "Plugin Drift Detector" }
func (p *PluginDriftDetector) Type() DriftType { return DriftPlugin }

func (p *PluginDriftDetector) Detect(ctx interface{}) ([]DriftFinding, error) {
	plugins, err := p.getPlugins()
	if err != nil {
		return nil, err
	}

	var findings []DriftFinding
	for pluginID, enabled := range plugins {
		if lastEnabled, exists := p.lastState[pluginID]; exists {
			if lastEnabled != enabled {
				findings = append(findings, DriftFinding{
					ID:          fmt.Sprintf("drift_plugin_%d", time.Now().UnixNano()),
					Type:        DriftPlugin,
					Severity:    SeverityMedium,
					Status:      StatusDetected,
					Resource:    pluginID,
					Description: fmt.Sprintf("Plugin %s state changed to %v", pluginID, enabled),
					DetectedAt:  time.Now(),
				})
			}
		}
	}

	p.lastState = plugins
	return findings, nil
}

// IntegrationDriftDetector detects endpoint/configuration changes
type IntegrationDriftDetector struct {
	getIntegrations func() (map[string]interface{}, error)
	lastState       map[string]interface{}
}

// NewIntegrationDriftDetector creates a new integration drift detector
func NewIntegrationDriftDetector(getIntegrations func() (map[string]interface{}, error)) *IntegrationDriftDetector {
	return &IntegrationDriftDetector{
		getIntegrations: getIntegrations,
		lastState:       make(map[string]interface{}),
	}
}

func (i *IntegrationDriftDetector) ID() string       { return "detector_integration_drift" }
func (i *IntegrationDriftDetector) Name() string     { return "Integration Drift Detector" }
func (i *IntegrationDriftDetector) Type() DriftType { return DriftIntegration }

func (i *IntegrationDriftDetector) Detect(ctx interface{}) ([]DriftFinding, error) {
	integrations, err := i.getIntegrations()
	if err != nil {
		return nil, err
	}

	var findings []DriftFinding
	for id, config := range integrations {
		if lastConfig, exists := i.lastState[id]; exists {
			if lastConfig != config {
				findings = append(findings, DriftFinding{
					ID:          fmt.Sprintf("drift_integration_%d", time.Now().UnixNano()),
					Type:        DriftIntegration,
					Severity:    SeverityHigh,
					Status:      StatusDetected,
					Resource:    id,
					Description: fmt.Sprintf("Integration endpoint changed for %s", id),
					DetectedAt:  time.Now(),
				})
			}
		}
	}

	i.lastState = integrations
	return findings, nil
}

// SchedulerDriftDetector detects job modifications
type SchedulerDriftDetector struct {
	getJobs   func() (map[string]interface{}, error)
	lastState map[string]interface{}
}

// NewSchedulerDriftDetector creates a new scheduler drift detector
func NewSchedulerDriftDetector(getJobs func() (map[string]interface{}, error)) *SchedulerDriftDetector {
	return &SchedulerDriftDetector{
		getJobs:   getJobs,
		lastState: make(map[string]interface{}),
	}
}

func (s *SchedulerDriftDetector) ID() string       { return "detector_scheduler_drift" }
func (s *SchedulerDriftDetector) Name() string     { return "Scheduler Drift Detector" }
func (s *SchedulerDriftDetector) Type() DriftType { return DriftScheduler }

func (s *SchedulerDriftDetector) Detect(ctx interface{}) ([]DriftFinding, error) {
	jobs, err := s.getJobs()
	if err != nil {
		return nil, err
	}

	var findings []DriftFinding
	for jobID := range jobs {
		if _, exists := s.lastState[jobID]; !exists {
			findings = append(findings, DriftFinding{
				ID:          fmt.Sprintf("drift_scheduler_%d", time.Now().UnixNano()),
				Type:        DriftScheduler,
				Severity:    SeverityMedium,
				Status:      StatusDetected,
				Resource:    jobID,
				Description: fmt.Sprintf("Job %s configuration changed", jobID),
				DetectedAt:  time.Now(),
			})
		}
	}

	s.lastState = jobs
	return findings, nil
}
