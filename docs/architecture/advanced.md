# DSO Advanced Platform

The advanced platform layer adds stable, production-ready platform extensions that enhance DSO's capabilities beyond basic secret injection.

## Capabilities

The advanced layer provides mature platform extensions:

- **Policy Engine**: Declarative policy rules for secret management, compliance validation, and enforcement
- **Drift Detection**: Continuous monitoring for configuration drift, unauthorized changes, and reconciliation
- **Dependency Graph**: Service dependency mapping, critical path analysis, and blast radius assessment

## Stability and Maturity

The advanced layer:

- ✓ Is production-ready and stable
- ✓ Can only depend on the core layer
- ✓ Has zero dependencies on the intelligence layer
- ✓ Gracefully degrades when core is healthy but advanced fails
- ✓ Includes comprehensive test coverage
- ✓ Is versioned with semantic versioning

## Design Principles

Advanced features follow these principles:

1. **Isolation**: Advanced subsystems are independent of each other
2. **Composability**: Features can be used together or independently
3. **Graceful Degradation**: Advanced features failing must not break core DSO
4. **Metrics First**: All features include operational metrics and observability
5. **Auditability**: All significant operations are logged to the audit trail

## Database Schema

All advanced features use the same SQLite database as core DSO, with dedicated migrations:

- Migration 0022: Policy Engine
- Migration 0023: Drift Detection
- Migration 0024: Dependency Graph

## API Integration

All advanced features expose REST API handlers:

- `internal/api/policy_handler.go`
- `internal/api/drift_handler.go`
- `internal/api/graph_handler.go`

## UI Integration

Advanced features are exposed in the web UI:

- Policy management dashboard
- Drift detection monitoring
- Dependency graph visualization

## Branch

Advanced platform development occurs on:
- `advanced-platform` (primary development branch)

This branch is always forward-compatible with `feature/web-ui` and `main`.

## Future Work

After CNCF acceptance, advanced layer will:
1. Gain feature flags for selective enablement
2. Use StartAdvanced() initialization hook
3. Potentially move to `internal/advanced/` package structure

## Event Integration

Advanced features integrate with core EventBus:

```
Events published:
- rule.started, rule.succeeded, rule.failed
- drift.detected, drift.acknowledged, drift.resolved
- drift.scan_started, drift.scan_completed
- graph.updated, graph.cycle_detected, graph.critical_node_detected
```
