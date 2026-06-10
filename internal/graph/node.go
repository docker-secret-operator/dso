package graph

// NodeType represents the type of graph node
type NodeType string

const (
	NodeSecret       NodeType = "secret"
	NodePolicy       NodeType = "policy"
	NodePlugin       NodeType = "plugin"
	NodeIntegration  NodeType = "integration"
	NodeUser         NodeType = "user"
	NodeSession      NodeType = "session"
	NodeSchedulerJob NodeType = "scheduler_job"
	NodeAlert        NodeType = "alert"
	NodeBackup       NodeType = "backup"
	NodeExecution    NodeType = "execution"
	NodeReview       NodeType = "review"
	NodeApproval     NodeType = "approval"
	NodeDrift        NodeType = "drift"
	NodeMetric       NodeType = "metric"
	NodeSecurity     NodeType = "security"
	NodeNotification NodeType = "notification"
)

// NodeStatus represents the health status of a node
type NodeStatus string

const (
	StatusHealthy NodeStatus = "healthy"
	StatusWarning NodeStatus = "warning"
	StatusFailed  NodeStatus = "failed"
	StatusUnknown NodeStatus = "unknown"
)

// Node represents a node in the dependency graph
type Node struct {
	ID       string
	Type     NodeType
	Name     string
	Status   NodeStatus
	Metadata map[string]string
}

// NewNode creates a new graph node
func NewNode(id string, nodeType NodeType, name string) *Node {
	return &Node{
		ID:       id,
		Type:     nodeType,
		Name:     name,
		Status:   StatusHealthy,
		Metadata: make(map[string]string),
	}
}
