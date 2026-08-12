package api

type AgentRequest struct {
	Provider string
	Config   map[string]string
	Secret   string
}

type AgentResponse struct {
	Data  map[string]string
	Error string
}

// AgentAPI is the interface exposed by the DSO agent daemon over Unix socket.
type AgentAPI interface {
	GetSecret(req *AgentRequest, resp *AgentResponse) error
	TriggerReconciliation(req *ReconcileRequest, resp *ReconcileResponse) error
	GetStatus(req *StatusRequest, resp *StatusResponse) error
	CheckProviderConnectivity(req *ProviderCheckRequest, resp *ProviderCheckResponse) error
}

// ReconcileRequest asks the agent to re-fetch and, if changed, re-rotate one
// or all configured secrets right now instead of waiting for the next poll.
type ReconcileRequest struct {
	// Secret restricts reconciliation to a single secret by name. Empty
	// means "all secrets configured on the agent".
	Secret string
}

// ReconcileResult reports the outcome for a single secret checked during
// reconciliation.
type ReconcileResult struct {
	Secret  string
	Rotated bool // true if the fetched value differed from cache and a rotation was triggered
	Error   string
}

type ReconcileResponse struct {
	SecretsChecked int
	SecretsRotated int
	Results        []ReconcileResult
}

// StatusRequest carries no fields; it exists so GetStatus fits net/rpc's
// func(argType, *replyType) error calling convention.
type StatusRequest struct{}

// ProviderStatusInfo reports what the agent's own SecretStoreManager
// currently knows about one configured provider's health. "Known" is false
// when the agent has never actually contacted that provider yet (e.g. it
// backs no currently-used secret), which is a distinct state from unhealthy.
type ProviderStatusInfo struct {
	Name    string
	Known   bool
	Healthy bool
	Message string
}

type StatusResponse struct {
	CacheEntries     int
	PendingRotations int
	Providers        []ProviderStatusInfo
}

// ProviderCheckRequest carries the exact provider configuration to test —
// not necessarily one the agent has already loaded, since this backs
// `docker dso apply`'s pre-flight check against a NEW config the agent hasn't
// applied yet.
type ProviderCheckRequest struct {
	ProviderName string
	Type         string
	Region       string
	Config       map[string]string
	AuthMethod   string
	AuthParams   map[string]string
}

type ProviderCheckResponse struct {
	Reachable bool
	Error     string
}
