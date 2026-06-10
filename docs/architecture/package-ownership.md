# Package Ownership and Layering

This document maps DSO packages to architectural layers, defining clear boundaries and dependencies.

## Package Layer Matrix

| Package | Layer | Type | Status | Stability |
|---------|-------|------|--------|-----------|
| execution | Core | Runtime | Stable | Production |
| scheduler | Core | Subsystem | Stable | Production |
| auth | Core | Security | Stable | Production |
| audit | Core | Observability | Stable | Production |
| backup | Core | Operations | Stable | Production |
| alerts | Core | Monitoring | Stable | Production |
| plugins | Core | Extension | Stable | Production |
| integrations | Core | Extension | Stable | Production |
| server | Core | Framework | Stable | Production |
| webui | Core | UI | Stable | Production |
| policy | Advanced | Governance | Stable | Production |
| drift | Advanced | Monitoring | Stable | Production |
| graph | Advanced | Analytics | Stable | Production |
| correlation | Intelligence | Analytics | Experimental | Experimental |
| recommendation | Intelligence | Intelligence | Experimental | Experimental |
| forecast | Intelligence | Forecasting | Experimental | Experimental |
| autonomy | Intelligence | Operations | Experimental | Experimental |

## Dependency Rules

### Core Layer
- ✓ May depend on: standard library, go.uber.org/zap, encoding/json, database/sql, net/http
- ✗ Must NOT depend on: advanced or intelligence packages

### Advanced Layer
- ✓ May depend on: core packages, standard library, and external dependencies
- ✗ Must NOT depend on: intelligence packages
- ✓ Must handle graceful degradation when core is healthy

### Intelligence Layer
- ✓ May depend on: core packages, advanced packages, standard library, and external dependencies
- ✓ Must handle graceful degradation when core or advanced are healthy
- ✓ Must isolate failures (panic recovery, logging, metrics)

## Initialization Order

Subsystems must initialize in layer order:

```
1. Core initialization
   - Database and migrations
   - Authentication
   - Audit logging
   - Plugins and integrations

2. Advanced initialization (optional)
   - Policy engine
   - Drift detection
   - Dependency graph

3. Intelligence initialization (optional)
   - Correlation engine
   - Recommendation engine
   - Forecasting engine
   - Autonomous operations
```

## Import Structure

Current structure (no refactoring yet):

```
internal/
  ├── execution/       (Core)
  ├── scheduler/       (Core)
  ├── auth/            (Core)
  ├── audit/           (Core)
  ├── backup/          (Core)
  ├── alerts/          (Core)
  ├── plugins/         (Core)
  ├── integrations/    (Core)
  ├── server/          (Core)
  ├── webui/           (Core)
  ├── policy/          (Advanced)
  ├── drift/           (Advanced)
  ├── graph/           (Advanced)
  ├── correlation/     (Intelligence)
  ├── recommendation/  (Intelligence)
  ├── forecast/        (Intelligence)
  └── autonomy/        (Intelligence)
```

After Phase C (package restructuring):

```
internal/
  ├── core/
  │   ├── execution/
  │   ├── scheduler/
  │   ├── auth/
  │   ├── audit/
  │   ├── backup/
  │   ├── alerts/
  │   ├── plugins/
  │   ├── integrations/
  │   ├── server/
  │   └── webui/
  ├── advanced/
  │   ├── policy/
  │   ├── drift/
  │   └── graph/
  └── intelligence/
      ├── correlation/
      ├── recommendation/
      ├── forecast/
      └── autonomy/
```

## Enforcement Strategy

Enforcement occurs in phases (after CNCF):

| Phase | Mechanism | Timeline |
|-------|-----------|----------|
| A | Feature flags | Immediate |
| B | Initialization layer separation | Week 1-2 |
| C | Package restructuring | Week 2-4 |
| D | Dependency validation (go mod, linters) | Week 4-5 |
| E | CI/CD checks (pre-commit, build gates) | Week 5-6 |

## Ownership and Maintenance

| Layer | Owner | Maintenance Model |
|-------|-------|-------------------|
| Core | Core maintainers | Long-term support, strict review |
| Advanced | Feature maintainers | Standard maintenance, semi-stable |
| Intelligence | Research/experimental team | Exploratory, rapid iteration |

## Testing Strategy

Each layer requires different testing approaches:

- **Core**: Integration tests, concurrency tests, failure scenarios
- **Advanced**: Unit tests + integration tests, feature isolation tests
- **Intelligence**: Unit tests, simulation tests, safety mechanism validation

## Breaking Changes

- **Core**: Breaking changes require major version bump and deprecation period
- **Advanced**: Breaking changes require minor version bump
- **Intelligence**: Breaking changes acceptable with notice (experimental status)
