# DSO Layered Architecture

Docker Secret Operator (DSO) is designed as a layered platform where each layer builds upon the previous one, enabling users to adopt only what they need.

## Architecture Overview

```
┌─────────────────────────────────────────┐
│   Intelligence Pack (Experimental)      │
│ • Correlation • Recommendation          │
│ • Forecasting • Autonomous Operations   │
└──────────────┬──────────────────────────┘
               ↑
┌──────────────┴──────────────────────────┐
│    Advanced Platform (Production)       │
│ • Policy Engine • Drift Detection       │
│ • Dependency Graph                      │
└──────────────┬──────────────────────────┘
               ↑
┌──────────────┴──────────────────────────┐
│   Core Platform (Stable - CNCF)         │
│ • Secrets • Execution • Scheduler       │
│ • Auth • Audit • Plugins • UI           │
└─────────────────────────────────────────┘
```

## Design Principles

### 1. Layered Independence
Each layer builds upon the previous without creating circular dependencies. Core can run without advanced or intelligence. Advanced can run without intelligence.

### 2. Graceful Degradation
Failures in upper layers must never impact lower layers. If the intelligence pack fails, the advanced platform and core continue operating normally.

### 3. Optional Adoption
Users can enable or disable subsystems based on their needs. A minimal DSO deployment includes only core. Advanced and intelligence features are opt-in.

### 4. Feature Flags
Each subsystem can be toggled via configuration:

```yaml
features:
  # Advanced layer (default: enabled)
  policy_engine: true
  drift_detection: true
  dependency_graph: true
  
  # Intelligence layer (default: disabled)
  correlation_engine: false
  recommendation_engine: false
  forecasting_engine: false
  autonomy_engine: false
```

### 5. Strict Dependency Rules
- Core packages depend only on standard library and vetted dependencies
- Advanced packages depend on core but not intelligence
- Intelligence packages depend on core and advanced

## Layers

### Core Platform (Production-Ready)

The stable foundation of DSO for runtime secret injection.

**Capabilities**:
- Secret storage, retrieval, and lifecycle management
- Execution orchestration and planning
- Job scheduling and maintenance
- Authentication and RBAC
- Complete audit logging
- Metrics and observability
- Backup and recovery
- Alert rules and notifications
- Plugin framework
- Third-party integrations
- Embedded Next.js UI

**Stability**: Production-ready, long-term supported

**Branch**: `main` (under CNCF review), `feature/web-ui` (development)

See: [docs/architecture/core.md](docs/architecture/core.md)

### Advanced Platform (Production-Ready)

Mature platform extensions for governance and visibility.

**Capabilities**:
- Policy engine for declarative governance
- Drift detection for configuration monitoring
- Dependency graph for impact analysis

**Stability**: Production-ready, stable

**Branch**: `advanced-platform`

See: [docs/architecture/advanced.md](docs/architecture/advanced.md)

### Intelligence Pack (Experimental)

Research and development features for intelligent operations.

**Capabilities**:
- Incident correlation and root cause analysis
- Intelligent recommendations for remediation
- Time-series forecasting and anomaly detection
- Autonomous self-healing operations with audit trails

**Stability**: Experimental, subject to breaking changes

**Branch**: `intelligence-pack`

See: [docs/architecture/intelligence.md](docs/architecture/intelligence.md)

## Package Organization

| Package | Layer | Purpose |
|---------|-------|---------|
| execution | Core | Secret injection execution engine |
| scheduler | Core | Job scheduling and triggers |
| auth | Core | Authentication and authorization |
| audit | Core | Immutable audit logging |
| backup | Core | Backup and restore operations |
| alerts | Core | Alert rules and notifications |
| plugins | Core | Plugin lifecycle and management |
| integrations | Core | External system integrations |
| policy | Advanced | Policy evaluation and enforcement |
| drift | Advanced | Configuration drift detection |
| graph | Advanced | Service dependency mapping |
| correlation | Intelligence | Incident correlation |
| recommendation | Intelligence | Remediation recommendations |
| forecast | Intelligence | Time-series forecasting |
| autonomy | Intelligence | Autonomous operations |

See: [docs/architecture/package-ownership.md](docs/architecture/package-ownership.md)

## Branch Strategy

DSO maintains four long-lived branches with a clear merge direction:

```
main (stable, CNCF)
  ↑
  │
feature/web-ui (development)
  ↑
  │
advanced-platform (platform staging)
  ↑
  │
intelligence-pack (experimental)
```

**Merge Direction**: Always upward. Never cherry-pick across branches.

See: [docs/architecture/branch-strategy.md](docs/architecture/branch-strategy.md)

## Evolution Roadmap

DSO will evolve in phases after CNCF acceptance:

### Phase A: Feature Flags (Weeks 1-2)
Enable/disable subsystems at runtime via configuration

### Phase B: Initialization Layers (Weeks 3-4)
Separate startup: `StartCore()`, `StartAdvanced()`, `StartIntelligence()`

### Phase C: Package Restructuring (Weeks 5-7)
Move code to `internal/core/`, `internal/advanced/`, `internal/intelligence/`

### Phase D: Dependency Validation (Weeks 7-8)
Automated checks prevent unauthorized cross-layer dependencies

### Phase E: CI/CD Enforcement (Weeks 8-9)
Gate-keeping in pull requests and build pipelines

See: [docs/architecture/roadmap.md](docs/architecture/roadmap.md)

## Getting Started

### For Users

1. Deploy core DSO for basic secret injection
2. Optionally enable advanced features (policy, drift) in production
3. Evaluate intelligence features in staging/development

### For Developers

1. Read [core.md](docs/architecture/core.md) to understand foundations
2. Check [package-ownership.md](docs/architecture/package-ownership.md) for layering rules
3. Follow [branch-strategy.md](docs/architecture/branch-strategy.md) for contribution workflow
4. Review [roadmap.md](docs/architecture/roadmap.md) for future work

## Key Files

- **Configuration**: `internal/server/config.go`
- **Server initialization**: `internal/server/rest.go`
- **Event system**: `internal/plugins/events.go`
- **Database migrations**: `internal/storage/sqlite/migrations.go`
- **API handlers**: `internal/api/`
- **Web UI**: `web/app/`

## Failure Modes and Guarantees

### Core Layer Failure
- ❌ Complete system failure (core must always work)
- ✓ Audit trail preserved
- ✓ Graceful shutdown

### Advanced Layer Failure
- ✓ Core continues operating
- ✓ Policy engine unavailable (but not enforced)
- ✓ Drift detection disabled (but core rules still enforce)
- ✓ Dependency graph unavailable
- ✓ Alerts continue firing for core events

### Intelligence Layer Failure
- ✓ Core continues operating
- ✓ Advanced layer continues operating
- ✓ No incident correlation (but events still flow)
- ✓ No recommendations (but human operators can act)
- ✓ No autonomous actions (but users can manual trigger fixes)
- ✓ No forecasts (but historical trends available)

## Testing Strategy

| Layer | Test Type | Coverage |
|-------|-----------|----------|
| Core | Unit + Integration | 80%+ |
| Advanced | Unit + Integration | 70%+ |
| Intelligence | Unit + Simulation | 60%+ |

All layers tested for:
- Graceful degradation (feature disabled)
- Panic recovery
- Metric accuracy
- Audit trail completeness

## Performance Characteristics

| Layer | Latency Impact | Memory Overhead | Notes |
|-------|---|---|---|
| Core | Baseline | Baseline | Direct request path |
| Advanced | +5-15ms | +20-50MB | Parallel processing |
| Intelligence | +50-200ms | +100-300MB | Async, background jobs |

All operations are non-blocking. Core request path unaffected by advanced/intelligence.

## Security Considerations

- **Authentication**: All layers inherit core auth
- **RBAC**: All operations subject to core access controls
- **Audit**: All layer operations logged to immutable audit trail
- **Secrets**: Intelligence features never see raw secret values
- **Isolation**: Intelligence autonomy operations reversible and audited

## Monitoring and Observability

### Metrics
Each layer exposes Prometheus metrics:
- `dso_core_*`: Core metrics
- `dso_advanced_*`: Advanced metrics
- `dso_intelligence_*`: Intelligence metrics

### Logs
Structured logging with correlation IDs across all layers

### Events
Event bus integration for real-time monitoring

See: `internal/plugins/events.go` for event types

## Contributing

1. Choose layer based on feature type (core/advanced/intelligence)
2. Follow [branch-strategy.md](docs/architecture/branch-strategy.md)
3. Ensure feature respects layer boundaries
4. Add tests and documentation
5. Create PR to appropriate branch

## Future Enhancements

- Machine learning model integration (intelligence)
- External analytics service connectors (intelligence)
- Workflow orchestration DSL (advanced/core)
- Custom remediation scripting (intelligence)
- Real-time policy simulation (advanced)

## Questions?

See architecture documentation:
- [Core Platform](docs/architecture/core.md)
- [Advanced Platform](docs/architecture/advanced.md)
- [Intelligence Pack](docs/architecture/intelligence.md)
- [Package Ownership](docs/architecture/package-ownership.md)
- [Roadmap](docs/architecture/roadmap.md)
- [Branch Strategy](docs/architecture/branch-strategy.md)
