// Package strategy implements the rotation strategy decision engine for DSO.
//
// The strategy decision engine analyzes container configurations and recommends
// an optimal rotation strategy for secret rotation. Two strategies are supported:
//
// 1. ROLLING STRATEGY (Zero-Downtime)
//    Starts a new container with updated secrets while the old one is still running,
//    then gracefully cuts over traffic. Requires high score (>= 70) indicating the
//    container can handle parallel execution safely.
//
// 2. RESTART STRATEGY (Brief Downtime)
//    Stops the old container and starts a new one with updated secrets. Used when
//    score is < 70, indicating the container has characteristics incompatible with
//    rolling updates (e.g., fixed port bindings, restart policies).
//
// Strategy Selection Scoring System:
//
// The decision engine uses a scoring system starting at a base of 100 points.
// Points are deducted based on container analysis factors that indicate risk
// during rolling updates. The final score determines strategy selection:
//
//   - Score >= 70: Rolling strategy (zero-downtime)
//   - Score < 70:  Restart strategy (brief downtime)
//
// Scoring Factors (Penalties):
//
//   - Fixed Port Binding (-50):     Containers listening on fixed ports cannot
//                                   run in parallel without port conflicts
//   - Explicit Container Name (-20): Docker enforces unique container names;
//                                   explicit names prevent scaling to 2 instances
//   - Restart Always Policy (-20):   restart:always may restart the old container
//                                   during rotation, causing conflicts
//   - No Health Check (-10):        Lack of health checks prevents safe cutover
//                                   validation and rollback decisions
//   - Stateful Workload (-20):      Stateful containers risk data corruption
//                                   when multiple instances run in parallel
//
// Example Scores:
//   - Stateless web service (no issues):           Score 100 → Rolling
//   - Service with no health check:                Score 90  → Rolling
//   - Service with health check + fixed port:      Score 50  → Restart
//   - Database/stateful + restart:always + port:   Score 10  → Restart
package strategy

import (
	"fmt"
	"strings"

	"github.com/docker-secret-operator/dso/internal/analyzer"
)

type StrategyDecision struct {
	Strategy string // rolling | restart
	Reason   string
	Score    int
	Report   string // Formatted report string
}

// DecideStrategy analyzes a container's configuration and recommends an optimal
// rotation strategy (rolling vs restart) for secret rotation.
//
// Input:
//   result: An AnalysisResult from the container analyzer containing:
//     - HasFixedPortBinding: Container listens on fixed ports (prevents parallel scaling)
//     - HasContainerName: Explicit container name specified (prevents scaling)
//     - HasRestartAlways: Restart policy set to "always" (risk of unexpected restarts)
//     - HasHealthCheck: Container defines a health check (enables safe cutover)
//     - IsStateful: Container identified as stateful (risk of data corruption)
//     - FixedPorts: List of fixed port bindings (for detailed reporting)
//     - ContainerName: Name of the container being analyzed
//
// Output:
//   StrategyDecision struct containing:
//     - Strategy: "rolling" (score >= 70) or "restart" (score < 70)
//     - Score: Numeric score (0-100) indicating suitability for rolling updates
//     - Reason: Human-readable explanation of score and penalties applied
//     - Report: Formatted analysis report combining analyzer findings and strategy decision
//
// Example Usage:
//
//   // Analyze a container
//   result := analyzer.AnalyzeContainer(container)
//
//   // Decide rotation strategy
//   decision := strategy.DecideStrategy(result)
//
//   if decision.Strategy == "rolling" {
//     // Perform zero-downtime rotation
//     executeRollingRotation(container, decision)
//   } else {
//     // Perform restart rotation (brief downtime)
//     executeRestartRotation(container, decision)
//   }
//
//   // Print detailed analysis report
//   fmt.Println(decision.Report)
func DecideStrategy(result analyzer.AnalysisResult) StrategyDecision {
	score := 100
	var reasons []string

	if result.HasFixedPortBinding {
		score -= 50
		reasons = append(reasons, "Fixed port binding prevents parallel containers")
	}
	if result.HasContainerName {
		score -= 20
		reasons = append(reasons, "Explicit container name conflicts with parallel scaling")
	}
	if result.HasRestartAlways {
		score -= 20
		reasons = append(reasons, "Restart policy conflicts with rotation engine")
	}
	if !result.HasHealthCheck {
		score -= 10
		reasons = append(reasons, "Lack of health check prevents safe cutover validation")
	}
	if result.IsStateful {
		score -= 20
		reasons = append(reasons, "Stateful workload detected (risk of data corruption during parallel run)")
	}

	decision := StrategyDecision{
		Score: score,
	}

	if score >= 70 {
		decision.Strategy = "rolling"
	} else {
		decision.Strategy = "restart"
	}

	if len(reasons) > 0 {
		decision.Reason = strings.Join(reasons, "\n- ")
	} else {
		decision.Reason = "Stateless, highly available workload"
	}

	// Format Analysis Report
	portStr := "NO"
	if result.HasFixedPortBinding && len(result.FixedPorts) > 0 {
		portStr = fmt.Sprintf("YES (%s)", strings.Join(result.FixedPorts, ", "))
	}
	restartStr := "NO"
	if result.HasRestartAlways {
		restartStr = "ALWAYS"
	}
	statefulStr := "NO"
	if result.IsStateful {
		statefulStr = "YES"
	}
	healthStr := "NO"
	if result.HasHealthCheck {
		healthStr = "YES"
	}

	analyzerLog := fmt.Sprintf("[DSO ANALYZER]\nContainer: %s\n- Fixed Port: %s\n- Restart Policy: %s\n- Stateful: %s\n- Health Check: %s",
		result.ContainerName, portStr, restartStr, statefulStr, healthStr)

	strategyLog := fmt.Sprintf("[DSO STRATEGY]\nSelected: %s\nScore: %d\nReason:\n- %s",
		decision.Strategy, decision.Score, decision.Reason)

	decision.Report = analyzerLog + "\n\n" + strategyLog

	return decision
}
