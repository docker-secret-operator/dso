package insights

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/docker-secret-operator/dso/internal/compliance"
	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/forecast"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// OperationalForecaster derives statistical risk forecasts from live evidence.
// No AI. No LLMs. No autonomy. Only statistics over observable history.
// Lives in `insights` (not `forecast`) to avoid the import cycle:
//   sqlite → forecast → compliance → sqlite
type OperationalForecaster struct {
	versionStore     *sqlite.SecretVersionStore
	driftStore       drift.Store
	complianceEngine *compliance.Engine
}

// NewOperationalForecaster creates a live forecaster.
func NewOperationalForecaster(
	vs *sqlite.SecretVersionStore,
	ds drift.Store,
	ce *compliance.Engine,
) *OperationalForecaster {
	return &OperationalForecaster{
		versionStore:     vs,
		driftStore:       ds,
		complianceEngine: ce,
	}
}

// ForecastAll returns all current operational forecasts sorted Critical → Low.
// Results are stateless: they disappear when the underlying evidence resolves.
func (f *OperationalForecaster) ForecastAll(ctx context.Context, secrets []compliance.SecretInput) []forecast.OperationalForecast {
	var all []forecast.OperationalForecast
	all = append(all, f.forecastRotation(ctx, secrets)...)
	all = append(all, f.forecastDrift(ctx)...)
	all = append(all, f.forecastCompliance(ctx, secrets)...)

	// Deduplicate by deterministic ID.
	seen := make(map[string]bool, len(all))
	unique := make([]forecast.OperationalForecast, 0, len(all))
	for _, fc := range all {
		if !seen[fc.ID] {
			seen[fc.ID] = true
			unique = append(unique, fc)
		}
	}

	sort.Slice(unique, func(i, j int) bool {
		return unique[i].Severity.Score() > unique[j].Severity.Score()
	})
	return unique
}

// ── Rotation forecasts ────────────────────────────────────────────────────────
//
// Algorithm:
//  1. 0 versions → warn (confidence 0.70)
//  2. 1 version → low confidence aging warning after 30 days
//  3. ≥2 versions → infer average rotation interval from history; if
//     elapsed/avgInterval ≥ 0.70 emit approaching-overdue forecast.
//     Confidence = f(versionCount, intervalConsistency).
func (f *OperationalForecaster) forecastRotation(ctx context.Context, secrets []compliance.SecretInput) []forecast.OperationalForecast {
	if f.versionStore == nil {
		return nil
	}
	now := time.Now().UTC()
	var out []forecast.OperationalForecast

	for _, s := range secrets {
		versions, err := f.versionStore.ListBySecret(ctx, s.Name)
		if err != nil || len(versions) == 0 {
			out = append(out, forecast.OperationalForecast{
				ID:          fmt.Sprintf("forecast:rotation:never:%s", s.Name),
				Category:    forecast.CatRotation,
				Severity:    forecast.SeverityMedium,
				Title:       fmt.Sprintf("%s has no rotation history", s.Name),
				Description: fmt.Sprintf("Secret %s has never been rotated. If rotation is required, this secret risks becoming non-compliant.", s.Name),
				Reason:      "No entries exist in the rotation version history. The secret may have been created before rotation tracking was introduced.",
				Resource:    s.Name,
				Confidence:  0.70,
				PredictedAt: now,
				Evidence: []string{
					"0 rotation events recorded in secret_versions",
					fmt.Sprintf("secret: %s, provider: %s", s.Name, s.Provider),
				},
			})
			continue
		}

		// versions are sorted newest-first by ListBySecret.
		lastRotatedAt := versions[0].CreatedAt

		if len(versions) < 2 {
			daysSince := now.Sub(lastRotatedAt).Hours() / 24
			if daysSince > 30 {
				out = append(out, forecast.OperationalForecast{
					ID:          fmt.Sprintf("forecast:rotation:aging:%s", s.Name),
					Category:    forecast.CatRotation,
					Severity:    forecast.SeverityLow,
					Title:       fmt.Sprintf("%s last rotated %.0f days ago (single event — no interval inferred)", s.Name, daysSince),
					Description: fmt.Sprintf("Only one rotation event recorded for %s. Without a second event, no rotation cadence can be modelled.", s.Name),
					Reason:      "A single data point cannot produce a trend. Add a second rotation event to enable interval-based forecasting.",
					Resource:    s.Name,
					Confidence:  0.55,
					PredictedAt: now,
					Evidence: []string{
						"1 rotation event recorded",
						fmt.Sprintf("last rotation: %s (%.0f days ago)", lastRotatedAt.Format("2006-01-02"), daysSince),
					},
				})
			}
			continue
		}

		intervals := rotationIntervals(versions)
		avgDays := fMean(intervals)
		consistency := 1.0 - fCoV(intervals)
		if consistency < 0 {
			consistency = 0
		}

		elapsed := now.Sub(lastRotatedAt)
		fraction := elapsed.Hours() / (avgDays * 24)
		daysUntilDue := avgDays - elapsed.Hours()/24

		if fraction < 0.70 {
			continue // Still comfortably within cycle.
		}

		confidence := rotationConfidence(len(intervals), consistency)

		severity := forecast.SeverityLow
		switch {
		case fraction >= 1.0:
			severity = forecast.SeverityHigh
		case fraction >= 0.90:
			severity = forecast.SeverityMedium
		}

		dueStr := fmt.Sprintf("due in ~%.0f day(s)", daysUntilDue)
		if daysUntilDue <= 0 {
			dueStr = "overdue now"
		}

		out = append(out, forecast.OperationalForecast{
			ID:          fmt.Sprintf("forecast:rotation:approaching:%s", s.Name),
			Category:    forecast.CatRotation,
			Severity:    severity,
			Title:       fmt.Sprintf("%s approaching rotation due (%.0f%% of inferred cycle elapsed)", s.Name, fraction*100),
			Description: fmt.Sprintf("Based on %d historical rotations, %s rotates every ~%.0f days on average. Last rotation was %.0f days ago (%s).", len(versions), s.Name, avgDays, elapsed.Hours()/24, dueStr),
			Reason:      fmt.Sprintf("%.0f%% of inferred rotation cycle elapsed. At ≥70%% the probability of missing the rotation SLA increases materially.", fraction*100),
			Resource:    s.Name,
			Confidence:  confidence,
			PredictedAt: now,
			Evidence: []string{
				fmt.Sprintf("%d total rotation events", len(versions)),
				fmt.Sprintf("average rotation interval: %.0f days", avgDays),
				fmt.Sprintf("last rotation: %s (%.0f days ago)", lastRotatedAt.Format("2006-01-02"), elapsed.Hours()/24),
				fmt.Sprintf("interval consistency: %.0f%% (coefficient of variation basis)", consistency*100),
			},
		})
	}
	return out
}

// ── Drift trend forecasts ─────────────────────────────────────────────────────
//
// Algorithm:
//  - Group findings by Resource.
//  - Rolling 14-day window.
//  - ≥3 findings in window → recurrence forecast.
//  - Confidence = min(count / 7.0, 0.95) — 7 events in window = 95% confidence.
func (f *OperationalForecaster) forecastDrift(ctx context.Context) []forecast.OperationalForecast {
	if f.driftStore == nil {
		return nil
	}
	findings, err := f.driftStore.ListFindings(ctx)
	if err != nil {
		return nil
	}

	now := time.Now().UTC()
	windowStart := now.Add(-14 * 24 * time.Hour)

	type resourceStats struct {
		recent     int
		severities []string
	}
	byResource := make(map[string]*resourceStats)
	for _, finding := range findings {
		if finding.DetectedAt.Before(windowStart) {
			continue
		}
		rs := byResource[finding.Resource]
		if rs == nil {
			rs = &resourceStats{}
			byResource[finding.Resource] = rs
		}
		rs.recent++
		rs.severities = append(rs.severities, string(finding.Severity))
	}

	var out []forecast.OperationalForecast
	for resource, rs := range byResource {
		if rs.recent < 3 {
			continue
		}

		confidence := math.Min(float64(rs.recent)/7.0, 0.95)
		severity := forecast.SeverityMedium
		if rs.recent >= 7 {
			severity = forecast.SeverityHigh
		}

		out = append(out, forecast.OperationalForecast{
			ID:          fmt.Sprintf("forecast:drift:recurrence:%s", resource),
			Category:    forecast.CatDrift,
			Severity:    severity,
			Title:       fmt.Sprintf("%s has recurring drift (%d events in 14 days)", resource, rs.recent),
			Description: fmt.Sprintf("%s generated %d drift findings in the last 14 days. This pattern suggests drift will recur if the root cause is not addressed.", resource, rs.recent),
			Reason:      fmt.Sprintf("Recurring drift: %d events in a 14-day window exceeds the recurrence threshold of 3.", rs.recent),
			Resource:    resource,
			Confidence:  confidence,
			PredictedAt: now,
			Evidence: []string{
				fmt.Sprintf("%d drift events on %s in the last 14 days", rs.recent, resource),
				fmt.Sprintf("14-day window: %s → %s", windowStart.Format("2006-01-02"), now.Format("2006-01-02")),
				fmt.Sprintf("severities observed: %v", uniqueStr(rs.severities)),
			},
		})
	}
	return out
}

// ── Compliance forecasts ──────────────────────────────────────────────────────
//
// Algorithm:
//  - Run ComplianceEngine.EvaluateAll to get current per-secret compliance.
//  - Warning-state secrets → at risk of non-compliance.
//  - ≥20% of estate non-compliant → systemic escalation.
func (f *OperationalForecaster) forecastCompliance(ctx context.Context, secrets []compliance.SecretInput) []forecast.OperationalForecast {
	if f.complianceEngine == nil || len(secrets) == 0 {
		return nil
	}

	records := f.complianceEngine.EvaluateAll(ctx, secrets)
	now := time.Now().UTC()

	var atRisk []string
	var nonCompliant int
	for _, r := range records {
		switch r.OverallStatus {
		case compliance.StatusNonCompliant:
			nonCompliant++
		case compliance.StatusWarning:
			atRisk = append(atRisk, r.SecretName)
		}
	}

	var out []forecast.OperationalForecast

	if len(atRisk) > 0 {
		conf := complianceForecastConf(len(atRisk), len(secrets))
		out = append(out, forecast.OperationalForecast{
			ID:          "forecast:compliance:warning-pool",
			Category:    forecast.CatCompliance,
			Severity:    forecast.SeverityMedium,
			Title:       fmt.Sprintf("%d secret(s) in warning state may become non-compliant", len(atRisk)),
			Description: fmt.Sprintf("%d secret(s) currently show a compliance warning. If the underlying rotation or drift issues are not resolved, they will transition to non-compliant.", len(atRisk)),
			Reason:      "Warning-state secrets have either an unknown rotation status or open drift findings. Without intervention the condition is likely to persist or worsen.",
			Resource:    "",
			Confidence:  conf,
			PredictedAt: now,
			Evidence: []string{
				fmt.Sprintf("%d secrets in compliance warning state", len(atRisk)),
				fmt.Sprintf("%d secrets already non-compliant", nonCompliant),
				fmt.Sprintf("total secrets evaluated: %d", len(secrets)),
				fmt.Sprintf("at-risk: %v", truncateList(atRisk, 5)),
			},
		})
	}

	if nonCompliant > 0 && len(secrets) > 0 {
		pct := float64(nonCompliant) / float64(len(secrets))
		if pct >= 0.20 {
			out = append(out, forecast.OperationalForecast{
				ID:          "forecast:compliance:mass-noncompliant",
				Category:    forecast.CatCompliance,
				Severity:    forecast.SeverityHigh,
				Title:       fmt.Sprintf("%.0f%% of secrets are non-compliant (%d of %d)", pct*100, nonCompliant, len(secrets)),
				Description: fmt.Sprintf("%d of %d managed secrets are currently non-compliant. At this scale, per-secret remediation is insufficient — a systemic fix is needed.", nonCompliant, len(secrets)),
				Reason:      "Non-compliance affecting ≥20% of the managed estate is a systemic signal, not an isolated incident.",
				Resource:    "",
				Confidence:  0.95,
				PredictedAt: now,
				Evidence: []string{
					fmt.Sprintf("%d non-compliant secrets", nonCompliant),
					fmt.Sprintf("%.0f%% of managed estate", pct*100),
					fmt.Sprintf("total secrets: %d", len(secrets)),
				},
			})
		}
	}

	return out
}

// ── Statistical helpers ───────────────────────────────────────────────────────

// rotationIntervals returns gaps in days between consecutive version entries.
// versions must be sorted newest-first (as returned by ListBySecret).
func rotationIntervals(versions []*sqlite.SecretVersion) []float64 {
	if len(versions) < 2 {
		return nil
	}
	out := make([]float64, 0, len(versions)-1)
	for i := 0; i < len(versions)-1; i++ {
		gap := versions[i].CreatedAt.Sub(versions[i+1].CreatedAt).Hours() / 24
		if gap > 0 {
			out = append(out, gap)
		}
	}
	return out
}

func fMean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var s float64
	for _, v := range vals {
		s += v
	}
	return s / float64(len(vals))
}

func fStddev(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	m := fMean(vals)
	var v float64
	for _, x := range vals {
		d := x - m
		v += d * d
	}
	return math.Sqrt(v / float64(len(vals)))
}

// fCoV returns stddev/mean (coefficient of variation); 0 if mean==0.
func fCoV(vals []float64) float64 {
	m := fMean(vals)
	if m == 0 {
		return 0
	}
	return fStddev(vals) / m
}

// rotationConfidence combines version count and interval consistency.
func rotationConfidence(versionCount int, consistency float64) float64 {
	base := 0.50
	switch {
	case versionCount >= 10:
		base = 0.85
	case versionCount >= 5:
		base = 0.75
	case versionCount >= 3:
		base = 0.65
	}
	conf := base + consistency*0.10
	if conf > 0.95 {
		conf = 0.95
	}
	return conf
}

// complianceForecastConf scales with the fraction of the estate at risk.
func complianceForecastConf(atRisk, total int) float64 {
	if total == 0 {
		return 0.50
	}
	conf := 0.50 + float64(atRisk)/float64(total)*0.45
	if conf > 0.95 {
		conf = 0.95
	}
	return conf
}

func uniqueStr(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func truncateList(ss []string, n int) []string {
	if len(ss) <= n {
		return ss
	}
	result := make([]string, n+1)
	copy(result, ss[:n])
	result[n] = fmt.Sprintf("...and %d more", len(ss)-n)
	return result
}
