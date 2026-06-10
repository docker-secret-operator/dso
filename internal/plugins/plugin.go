package plugins

import (
	"context"
	"time"
)

// PluginType defines the category of a plugin
type PluginType string

const (
	TypeProvider      PluginType = "provider"
	TypeExporter      PluginType = "exporter"
	TypeAnalyzer      PluginType = "analyzer"
	TypeAction        PluginType = "action"
	TypeAlert         PluginType = "alert"
	TypeMetric        PluginType = "metric"
	TypeNotification  PluginType = "notification"
)

// Capability defines what a plugin can do
type Capability string

const (
	CapabilityMetrics       Capability = "metrics"
	CapabilityAlert         Capability = "alert"
	CapabilitySecurity      Capability = "security"
	CapabilityBackup        Capability = "backup"
	CapabilityExport        Capability = "export"
	CapabilityNotification  Capability = "notification"
	CapabilityAction        Capability = "action"
)

// PluginHealth represents the health state of a plugin
type PluginHealth string

const (
	HealthHealthy   PluginHealth = "healthy"
	HealthDegraded  PluginHealth = "degraded"
	HealthFailed    PluginHealth = "failed"
	HealthDisabled  PluginHealth = "disabled"
)

// Plugin is the interface all plugins must implement
type Plugin interface {
	// Metadata
	ID() string
	Name() string
	Version() string
	Description() string
	Type() PluginType

	// Capabilities and dependencies
	Capabilities() []Capability
	Dependencies() []string

	// Lifecycle
	Initialize(ctx context.Context) error
	Shutdown(ctx context.Context) error

	// Health monitoring
	Health() PluginHealth
	Heartbeat() error
}

// PluginStatus represents the runtime state of a plugin
type PluginStatus string

const (
	StatusEnabled  PluginStatus = "enabled"
	StatusDisabled PluginStatus = "disabled"
	StatusFailed   PluginStatus = "failed"
)

// PluginMetadata holds runtime information about a plugin
type PluginMetadata struct {
	ID              string
	Name            string
	Version         string
	Type            PluginType
	Description     string
	Capabilities    []Capability
	Dependencies    []string
	Enabled         bool
	Status          PluginStatus
	Health          PluginHealth
	ErrorMessage    *string
	LoadedAt        *time.Time
	EnabledAt       *time.Time
	DisabledAt      *time.Time
	LastErrorTime   *time.Time
	LastHeartbeat   *time.Time
	RestartCount    int
	EventCount      int
	UptimeMs        int64
	ErrorCount      int
}

// BasePlugin provides default implementations
type BasePlugin struct {
	id          string
	name        string
	version     string
	description string
	pluginType  PluginType
}

func NewBasePlugin(id, name, version, description string, pluginType PluginType) BasePlugin {
	return BasePlugin{
		id:          id,
		name:        name,
		version:     version,
		description: description,
		pluginType:  pluginType,
	}
}

func (bp BasePlugin) ID() string {
	return bp.id
}

func (bp BasePlugin) Name() string {
	return bp.name
}

func (bp BasePlugin) Version() string {
	return bp.version
}

func (bp BasePlugin) Description() string {
	return bp.description
}

func (bp BasePlugin) Type() PluginType {
	return bp.pluginType
}

func (bp BasePlugin) Initialize(ctx context.Context) error {
	return nil
}

func (bp BasePlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (bp BasePlugin) Capabilities() []Capability {
	return []Capability{}
}

func (bp BasePlugin) Dependencies() []string {
	return []string{}
}

func (bp BasePlugin) Health() PluginHealth {
	return HealthHealthy
}

func (bp BasePlugin) Heartbeat() error {
	return nil
}
