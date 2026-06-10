package plugins

import "time"

// IntegrationConfig holds configuration for an integration
type IntegrationConfig struct {
	PluginID       string
	Enabled        bool
	Endpoint       string
	AuthType       string // none, basic, bearer, api_key
	AuthConfigJSON string // JSON-encoded auth config
	FiltersJSON    string // JSON-encoded event filters
	RetryPolicyJSON string // JSON-encoded retry config
	UpdatedAt      time.Time
}

// RetryPolicy defines retry behavior
type RetryPolicy struct {
	MaxRetries      int           `json:"max_retries"`
	InitialBackoff  int           `json:"initial_backoff_seconds"`
	MaxBackoff      int           `json:"max_backoff_seconds"`
	BackoffMultiplier float64    `json:"backoff_multiplier"`
}

// EventFilter defines which events to process
type EventFilter struct {
	EventTypes []string `json:"event_types"`
	Severity   string   `json:"severity,omitempty"` // for alert events
}

// IntegrationDelivery tracks a delivery attempt
type IntegrationDelivery struct {
	ID            string
	PluginID      string
	EventType     string
	EventID       string
	Success       bool
	ResponseCode  int
	ErrorMessage  *string
	Attempt       int
	CreatedAt     time.Time
}

// DeliveryQueue item
type DeliveryQueueItem struct {
	ID               string
	IntegrationID    string
	Event            Event
	Attempt          int
	NextRetryTime    time.Time
	LastError        *string
	CreatedAt        time.Time
}

// IntegrationMetrics holds integration health metrics
type IntegrationMetrics struct {
	PluginID         string
	Enabled          bool
	TotalEvents      int
	SuccessfulCount  int
	FailedCount      int
	LastSuccessTime  *time.Time
	LastErrorTime    *time.Time
	LastError        *string
	AvgDeliveryMs    float64
}

// AuthConfig holds authentication details
type AuthConfig struct {
	Type       string `json:"type"` // basic, bearer, api_key
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	Token      string `json:"token,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	APISecret  string `json:"api_secret,omitempty"`
}
