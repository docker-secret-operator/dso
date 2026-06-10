# Layered Architecture Roadmap

This roadmap describes how DSO will evolve from a monolithic architecture into a clean, layered platform after CNCF acceptance.

## Current State (Pre-CNCF)

- ✓ All functionality integrated into `feature/web-ui` branch
- ✓ Core capabilities: execution, scheduling, auth, audit
- ✓ Advanced capabilities: policy, drift, graph
- ✓ Intelligence capabilities: correlation, recommendation, forecast, autonomy
- ⏸ No architectural separation (code, imports, initialization)
- ⏸ No feature flags
- ⏸ Tight coupling between layers

## Goal (Post-CNCF)

- Clear separation of concerns
- Optional advanced and intelligence subsystems
- Graceful degradation when subsystems are disabled
- Maintainable, extensible architecture
- Foundation for future platform evolution

## Phase A: Feature Flags (Weeks 1-2 after CNCF)

**Objective**: Enable selective subsystem enablement

**Work**:
```go
type FeatureConfig struct {
  // Advanced layer
  PolicyEngine        bool `json:"policy_engine" default:"true"`
  DriftDetection      bool `json:"drift_detection" default:"true"`
  DependencyGraph     bool `json:"dependency_graph" default:"true"`
  
  // Intelligence layer
  CorrelationEngine   bool `json:"correlation_engine" default:"false"`
  RecommendationEngine bool `json:"recommendation_engine" default:"false"`
  ForecastingEngine   bool `json:"forecasting_engine" default:"false"`
  AutonomyEngine      bool `json:"autonomy_engine" default:"false"`
}
```

**Integration Points**:
- Config file: `dso.yaml`
- Environment variables: `DSO_FEATURE_*`
- API: `/api/config/features`

**Validation**:
- ✓ Feature flags can be toggled at runtime
- ✓ Disabled subsystems don't initialize
- ✓ Tests pass with all combinations

## Phase B: Subsystem Initialization (Weeks 3-4 after CNCF)

**Objective**: Separate initialization into three logical phases

**Work**:

```go
// Core always initializes
func (srv *Server) StartCore(ctx context.Context) error {
  // Initialize core subsystems
  if err := srv.ExecutionEngine.Initialize(); err != nil {
    return fmt.Errorf("core initialization failed: %w", err)
  }
  // ... auth, audit, scheduler, etc.
  return nil
}

// Advanced initializes if enabled
func (srv *Server) StartAdvanced(ctx context.Context) error {
  if !srv.Config.Features.PolicyEngine {
    srv.Logger.Info("Advanced layer disabled, skipping")
    return nil
  }
  
  // Initialize advanced subsystems
  if err := srv.PolicyEngine.Initialize(); err != nil {
    srv.Logger.Warn("Advanced initialization failed", zap.Error(err))
    return nil // Don't fail core
  }
  return nil
}

// Intelligence initializes if enabled
func (srv *Server) StartIntelligence(ctx context.Context) error {
  if !srv.Config.Features.CorrelationEngine && 
     !srv.Config.Features.RecommendationEngine &&
     !srv.Config.Features.ForecastingEngine &&
     !srv.Config.Features.AutonomyEngine {
    srv.Logger.Info("Intelligence layer disabled, skipping")
    return nil
  }
  
  // Initialize intelligence subsystems
  // ... 
  return nil
}

// Main startup
func main() {
  srv := NewServer()
  if err := srv.StartCore(ctx); err != nil {
    log.Fatalf("core startup failed: %v", err)
  }
  srv.StartAdvanced(ctx) // Non-fatal
  srv.StartIntelligence(ctx) // Non-fatal
}
```

**Validation**:
- ✓ Core always starts successfully or fails completely
- ✓ Advanced/intelligence start failures don't affect core
- ✓ Graceful degradation when advanced/intelligence are disabled

## Phase C: Package Restructuring (Weeks 5-7 after CNCF)

**Objective**: Organize code by layer

**Work**:

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

**Constraints**:
- ✗ Core packages cannot import from advanced or intelligence
- ✗ Advanced packages cannot import from intelligence
- ✓ Advanced and intelligence can import from core
- ✓ All can import from standard library and vendored dependencies

**Validation**:
- ✓ `go build` succeeds
- ✓ No cyclic imports
- ✓ Linter detects unauthorized imports (custom rule)

## Phase D: Dependency Validation (Weeks 7-8 after CNCF)

**Objective**: Automated validation of layering rules

**Tools**:
- `go mod graph` for dependency analysis
- Custom linter rules (Go's `golang.org/x/lint`)
- Build-time validation

**Rules**:
```
if import_from_package.startsWith("internal/core"):
  error("Package hierarchy violation")

if import_from_package.startsWith("internal/intelligence"):
  if !importing_package.startsWith("internal/intelligence"):
    error("Intelligence packages cannot be imported by core or advanced")
```

**Validation**:
- ✓ Dependency checker runs during build
- ✓ No violations in codebase
- ✓ Developers receive clear error messages

## Phase E: CI/CD Enforcement (Weeks 8-9 after CNCF)

**Objective**: Automated checks in pull request and build pipelines

**Implementation**:

```yaml
# .github/workflows/build.yml
- name: Check package hierarchy
  run: |
    go mod graph | ./scripts/validate-layers.sh
    ./scripts/check-imports.sh
```

**Checks**:
- Package import validation
- Cyclic dependency detection
- Feature flag usage audit
- Test coverage for each layer
- Build-time assertion tests

**Validation**:
- ✓ PRs cannot merge without passing layer checks
- ✓ CI catches violations early
- ✓ Developers aware of boundaries

## Timeline Summary

| Phase | Duration | Status | Effort |
|-------|----------|--------|--------|
| Pre-CNCF | Current | Active | On-going |
| A | 2 weeks | Planned | Medium |
| B | 2 weeks | Planned | Medium |
| C | 3 weeks | Planned | High |
| D | 2 weeks | Planned | Medium |
| E | 2 weeks | Planned | Low |
| **Total** | **11 weeks** | | |

## Benefits by Phase

- **Phase A**: Operators can disable experimental features
- **Phase B**: Clear initialization semantics, graceful degradation
- **Phase C**: Maintainable codebase, easier onboarding
- **Phase D**: Automated validation, prevents regressions
- **Phase E**: Gate-keeping for pull requests, team alignment

## Success Criteria

✓ After Phase E:

1. Core layer is production-grade and stable
2. Advanced layer is optional but stable when enabled
3. Intelligence layer is experimental but safe
4. No feature in a lower layer depends on a higher layer
5. Failures in higher layers cannot affect lower layers
6. Clear documentation and ownership
7. Team discipline on layering is enforced by tooling
