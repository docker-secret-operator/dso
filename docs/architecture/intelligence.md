# DSO Intelligence Pack

The intelligence layer provides experimental, advanced analytics and autonomous capabilities that enhance DSO with intelligence-driven operations.

## Status

**⚠️ Experimental and optional.** These capabilities are:

- Research and development features
- Not yet recommended for production critical workloads
- Subject to breaking changes
- Candidates for future releases

## Capabilities

The intelligence layer provides:

- **Correlation Engine**: Incident correlation, root cause analysis, and incident grouping
- **Recommendation Engine**: Intelligent recommendations for remediation, optimization, and best practices
- **Forecasting Engine**: Time-series forecasting, anomaly detection, and trend analysis
- **Autonomous Operations**: Self-healing remediation with human control, safety levels, and audit trails

## Design Principles

Intelligence features follow these principles:

1. **Safety First**: Multi-level safety gates (manual-only, approval-required, automatic)
2. **Auditability**: Complete audit trail of all autonomous actions and decisions
3. **Reversibility**: Rollback capability for all autonomous operations
4. **Human Override**: All decisions can be reviewed, rejected, or rolled back
5. **Graceful Degradation**: Intelligence layer failures never affect core DSO or advanced platform

## Database Schema

All intelligence features use dedicated migrations:

- Migration 0025: Correlation Engine
- Migration 0026: Recommendation Engine
- Migration 0027: Forecasting Engine
- Migration 0028: Autonomous Operations

## API Integration

Intelligence features expose REST API handlers:

- `internal/api/correlation_handler.go`
- `internal/api/recommendation_handler.go`
- `internal/api/forecast_handler.go`
- `internal/api/autonomy_handler.go`

## UI Integration

Intelligence features are exposed in the web UI:

- Incidents dashboard (correlation)
- Recommendations panel (actions and insights)
- Forecasts and trends (analytics)
- Autonomy operations (action history and control)

## Branch

Intelligence development occurs on:
- `intelligence-pack` (primary development branch)

This branch is always forward-compatible with `advanced-platform`, `feature/web-ui`, and `main`.

## Deployment Strategy

Intelligence features should be deployed as follows:

1. **Development/Testing**: All features enabled for evaluation
2. **Staging**: Selected features enabled with human approval gates
3. **Production**: Features disabled by default; enable selectively with safety level tuning

## Future Work

After CNCF acceptance, intelligence layer will:

1. Gain feature flags for selective enablement (default: disabled)
2. Use StartIntelligence() initialization hook
3. Move to `internal/intelligence/` package structure
4. Add decision audit and explainability features
5. Support pluggable recommendation rules
6. Integrate with external ML/analytics services

## Failure Modes

Intelligence layer failures are designed to be isolated:

- If correlation fails: incidents still flow to alerts
- If recommendations fail: no suggestions provided, but monitoring continues
- If forecasting fails: latest known forecasts still available, operations continue
- If autonomy fails: no automatic actions, manual remediation still works

## Confidence and Maturity

These features represent the future direction of DSO but require:

- More production usage and feedback
- Performance optimization and scaling validation
- Integration testing with real-world incident patterns
- Safety mechanism hardening

## Event Integration

Intelligence features integrate with core EventBus:

```
Events published:
- incident.created, incident.updated, incident.resolved
- recommendation.created, recommendation.acknowledged, recommendation.implemented, recommendation.dismissed
- forecast.created, forecast.updated, forecast.critical_detected
- autonomy.action_started, autonomy.action_succeeded, autonomy.action_failed, autonomy.action_rolled_back
```
