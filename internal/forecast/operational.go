package forecast

import "time"

// OperationalCategory classifies a P9 forecast by domain.
type OperationalCategory string

const (
	CatRotation    OperationalCategory = "rotation"
	CatDrift       OperationalCategory = "drift"
	CatCompliance  OperationalCategory = "compliance"
	CatOperational OperationalCategory = "operational"
)

// OperationalForecast is the P9 forecast struct: statistical, explainable, evidence-based.
// It is computed at query time and never persisted. When evidence disappears the forecast
// disappears automatically on the next evaluation.
type OperationalForecast struct {
	// Deterministic ID computed from category+resource — forecast disappears when evidence resolves.
	ID string

	Category    OperationalCategory
	Severity    ForecastSeverity
	Title       string
	Description string

	// Confidence is a probability in [0,1] derived only from evidence count and statistical
	// consistency. It is never inflated by heuristics or magic numbers.
	Confidence float64

	PredictedAt time.Time

	// Evidence is the list of observable facts that produced this forecast.
	// Every number shown in the UI must trace back to one of these.
	Evidence []string

	// Reason states WHY the evidence leads to this prediction.
	Reason string

	// Resource is the secret name, policy ID, or resource the forecast concerns.
	Resource string
}
