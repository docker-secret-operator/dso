package graph

// Recommendation represents a recommendation for a node
type Recommendation struct {
	Type        string // "reduce_risk", "improve_resilience", "document", "optimize"
	Priority    string // "critical", "high", "medium", "low"
	Description string
	Action      string
}

// GetRecommendations generates recommendations for a node based on its characteristics
func (g *Graph) GetRecommendations(nodeID string) []Recommendation {
	node := g.GetNode(nodeID)
	if node == nil {
		return []Recommendation{}
	}

	var recommendations []Recommendation

	switch node.Type {
	case NodeSecret:
		recommendations = append(recommendations, g.getSecretRecommendations(nodeID)...)
	case NodePolicy:
		recommendations = append(recommendations, g.getPolicyRecommendations(nodeID)...)
	case NodePlugin:
		recommendations = append(recommendations, g.getPluginRecommendations(nodeID)...)
	case NodeIntegration:
		recommendations = append(recommendations, g.getIntegrationRecommendations(nodeID)...)
	case NodeUser:
		recommendations = append(recommendations, g.getUserRecommendations(nodeID)...)
	case NodeSession:
		recommendations = append(recommendations, g.getSessionRecommendations(nodeID)...)
	case NodeSchedulerJob:
		recommendations = append(recommendations, g.getSchedulerJobRecommendations(nodeID)...)
	case NodeAlert:
		recommendations = append(recommendations, g.getAlertRecommendations(nodeID)...)
	case NodeBackup:
		recommendations = append(recommendations, g.getBackupRecommendations(nodeID)...)
	case NodeExecution:
		recommendations = append(recommendations, g.getExecutionRecommendations(nodeID)...)
	case NodeReview:
		recommendations = append(recommendations, g.getReviewRecommendations(nodeID)...)
	case NodeApproval:
		recommendations = append(recommendations, g.getApprovalRecommendations(nodeID)...)
	case NodeDrift:
		recommendations = append(recommendations, g.getDriftRecommendations(nodeID)...)
	case NodeMetric:
		recommendations = append(recommendations, g.getMetricRecommendations(nodeID)...)
	case NodeSecurity:
		recommendations = append(recommendations, g.getSecurityRecommendations(nodeID)...)
	case NodeNotification:
		recommendations = append(recommendations, g.getNotificationRecommendations(nodeID)...)
	}

	// Add cross-cutting recommendations based on structure
	score := g.GetCriticalityScore(nodeID)
	if score > 10 {
		recommendations = append(recommendations, Recommendation{
			Type:        "improve_resilience",
			Priority:    "high",
			Description: "This node is critical with high centrality. Consider adding redundancy and monitoring.",
			Action:      "Review and enhance resilience measures",
		})
	}

	// Check for cycles
	cycles := g.DetectCycles()
	for _, cycle := range cycles {
		for _, n := range cycle {
			if n == nodeID {
				recommendations = append(recommendations, Recommendation{
					Type:        "reduce_risk",
					Priority:    "critical",
					Description: "This node is part of a circular dependency. This can cause cascading failures.",
					Action:      "Break the cycle by refactoring dependencies",
				})
				break
			}
		}
	}

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 20 {
		recommendations = append(recommendations, Recommendation{
			Type:        "reduce_risk",
			Priority:    "high",
			Description: "This node has many downstream dependents. Changes here affect a large portion of the system.",
			Action:      "Establish change control procedures and comprehensive testing",
		})
	}

	return recommendations
}

func (g *Graph) getSecretRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 5 {
		recs = append(recs, Recommendation{
			Type:        "reduce_risk",
			Priority:    "high",
			Description: "This secret is used by many services. Consider rotation strategy.",
			Action:      "Implement automated secret rotation",
		})
	}

	// Check for stale dependencies
	if len(dependents) == 0 {
		recs = append(recs, Recommendation{
			Type:        "optimize",
			Priority:    "medium",
			Description: "This secret doesn't appear to be used by any active services.",
			Action:      "Review and potentially remove unused secret",
		})
	}

	return recs
}

func (g *Graph) getPolicyRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	// Check if policy affects many resources
	dependents := g.GetDependents(nodeID)
	if len(dependents) > 10 {
		recs = append(recs, Recommendation{
			Type:        "document",
			Priority:    "high",
			Description: "This policy affects many resources. Ensure proper documentation.",
			Action:      "Create comprehensive policy documentation and runbook",
		})
	}

	return recs
}

func (g *Graph) getPluginRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependencies := g.GetDependencies(nodeID)
	if len(dependencies) > 5 {
		recs = append(recs, Recommendation{
			Type:        "optimize",
			Priority:    "medium",
			Description: "This plugin has many dependencies. Consider modularization.",
			Action:      "Review and potentially modularize plugin dependencies",
		})
	}

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 3 {
		recs = append(recs, Recommendation{
			Type:        "improve_resilience",
			Priority:    "high",
			Description: "This plugin is widely used. Implement graceful degradation.",
			Action:      "Add fallback mechanisms and circuit breakers",
		})
	}

	return recs
}

func (g *Graph) getIntegrationRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 5 {
		recs = append(recs, Recommendation{
			Type:        "improve_resilience",
			Priority:    "high",
			Description: "This integration is used by many services. Ensure high availability.",
			Action:      "Implement retry logic and failover mechanisms",
		})
	}

	return recs
}

func (g *Graph) getUserRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	// Check user's resource access scope
	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 20 {
		recs = append(recs, Recommendation{
			Type:        "reduce_risk",
			Priority:    "critical",
			Description: "User has access to many critical resources.",
			Action:      "Perform access review and apply principle of least privilege",
		})
	}

	return recs
}

func (g *Graph) getSessionRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	// Sessions should be temporary
	recs = append(recs, Recommendation{
		Type:        "reduce_risk",
		Priority:    "medium",
		Description: "Monitor session activity and ensure proper timeout configuration.",
		Action:      "Review session timeout policies and audit trails",
	})

	return recs
}

func (g *Graph) getSchedulerJobRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 5 {
		recs = append(recs, Recommendation{
			Type:        "improve_resilience",
			Priority:    "high",
			Description: "Job output affects many downstream processes.",
			Action:      "Implement comprehensive logging and monitoring",
		})
	}

	return recs
}

func (g *Graph) getAlertRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependents(nodeID)
	if len(dependents) > 0 {
		recs = append(recs, Recommendation{
			Type:        "improve_resilience",
			Priority:    "medium",
			Description: "Alert is actively being consumed. Ensure proper alert routing.",
			Action:      "Review alert notification channels and response procedures",
		})
	}

	return recs
}

func (g *Graph) getBackupRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	recs = append(recs, Recommendation{
		Type:        "improve_resilience",
		Priority:    "critical",
		Description: "Backup integrity is essential. Implement regular restore testing.",
		Action:      "Schedule and test regular restore procedures",
	})

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) == 0 {
		recs = append(recs, Recommendation{
			Type:        "optimize",
			Priority:    "low",
			Description: "This backup doesn't appear to be used for any active recovery plans.",
			Action:      "Review backup retention policy",
		})
	}

	return recs
}

func (g *Graph) getExecutionRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	// Check for execution in cycles
	cycles := g.DetectCycles()
	for _, cycle := range cycles {
		for _, n := range cycle {
			if n == nodeID {
				recs = append(recs, Recommendation{
					Type:        "reduce_risk",
					Priority:    "high",
					Description: "This execution is part of a dependency cycle.",
					Action:      "Review execution flow and break circular dependencies",
				})
				break
			}
		}
	}

	return recs
}

func (g *Graph) getReviewRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 10 {
		recs = append(recs, Recommendation{
			Type:        "document",
			Priority:    "medium",
			Description: "This review affects many resources. Maintain detailed documentation.",
			Action:      "Document review decision and impact analysis",
		})
	}

	return recs
}

func (g *Graph) getApprovalRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	recs = append(recs, Recommendation{
		Type:        "reduce_risk",
		Priority:    "high",
		Description: "Approval workflows should have clear audit trails.",
		Action:      "Implement comprehensive approval audit logging",
	})

	return recs
}

func (g *Graph) getDriftRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 0 {
		recs = append(recs, Recommendation{
			Type:        "reduce_risk",
			Priority:    "critical",
			Description: "Drift in this resource affects downstream services.",
			Action:      "Prioritize drift remediation and implement prevention measures",
		})
	}

	return recs
}

func (g *Graph) getMetricRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 5 {
		recs = append(recs, Recommendation{
			Type:        "improve_resilience",
			Priority:    "high",
			Description: "This metric is used for critical alerting. Ensure high availability.",
			Action:      "Implement redundant metric collection and storage",
		})
	}

	return recs
}

func (g *Graph) getSecurityRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependentsTransitive(nodeID)
	if len(dependents) > 3 {
		recs = append(recs, Recommendation{
			Type:        "reduce_risk",
			Priority:    "critical",
			Description: "Security event affects multiple systems. Ensure comprehensive response.",
			Action:      "Implement automated security incident response procedures",
		})
	}

	return recs
}

func (g *Graph) getNotificationRecommendations(nodeID string) []Recommendation {
	var recs []Recommendation

	dependents := g.GetDependents(nodeID)
	if len(dependents) > 5 {
		recs = append(recs, Recommendation{
			Type:        "improve_resilience",
			Priority:    "high",
			Description: "This notification channel is heavily used. Ensure reliability.",
			Action:      "Monitor notification delivery and implement retry logic",
		})
	}

	return recs
}
