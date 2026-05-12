package strategy

import (
	"testing"

	"github.com/docker-secret-operator/dso/internal/analyzer"
)

func TestDecideStrategy(t *testing.T) {
	tests := []struct {
		name             string
		result           analyzer.AnalysisResult
		expectedStrategy string
		minScore         int
	}{
		{
			name: "Stateless with healthcheck",
			result: analyzer.AnalysisResult{
				ContainerName:  "web",
				HasHealthCheck: true,
			},
			expectedStrategy: "rolling",
			minScore:         90,
		},
		{
			name: "Fixed port binding",
			result: analyzer.AnalysisResult{
				ContainerName:       "api",
				HasFixedPortBinding: true,
				FixedPorts:          []string{"8080"},
				HasHealthCheck:      true,
			},
			expectedStrategy: "restart",
			minScore:         0,
		},
		{
			name: "Stateful application",
			result: analyzer.AnalysisResult{
				ContainerName:  "db",
				IsStateful:     true,
				HasHealthCheck: true,
			},
			expectedStrategy: "rolling", // 100 - 20 = 80 >= 70
			minScore:         80,
		},
		{
			name: "Restart always policy",
			result: analyzer.AnalysisResult{
				ContainerName:    "worker",
				HasRestartAlways: true,
				HasHealthCheck:   true,
			},
			expectedStrategy: "rolling", // 100 - 20 = 80 >= 70
			minScore:         80,
		},
		{
			name: "Multiple risks",
			result: analyzer.AnalysisResult{
				ContainerName:    "risk",
				IsStateful:       true,  // -20
				HasRestartAlways: true,  // -20
				HasHealthCheck:   false, // -10
			},
			expectedStrategy: "restart", // 100 - 20 - 20 - 10 = 50 < 70
			minScore:         50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := DecideStrategy(tt.result)
			if decision.Strategy != tt.expectedStrategy {
				t.Errorf("DecideStrategy() strategy = %v, want %v", decision.Strategy, tt.expectedStrategy)
			}
			if decision.Score < tt.minScore {
				t.Errorf("DecideStrategy() score = %v, want min %v", decision.Score, tt.minScore)
			}
			if decision.Report == "" {
				t.Errorf("DecideStrategy() report is empty")
			}
		})
	}
}
