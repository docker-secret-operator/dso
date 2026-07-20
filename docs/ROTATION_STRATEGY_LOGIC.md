# DSO Rotation Strategy Decision Logic

## Overview

DSO (Docker Secret Operator) uses an intelligent decision engine to automatically select the optimal rotation strategy for each container. The selection is based on analyzing the container's configuration and characteristics to determine whether it can safely handle a zero-downtime rolling update or if it requires a brief-downtime restart strategy.

This document explains how DSO makes this critical decision and provides operational insights for understanding rotation behavior.

## Supported Strategies

### 1. Rolling Strategy (Zero-Downtime)

**Definition**: Starts a new container with updated secrets alongside the old container, validates readiness, and then gracefully cuts over traffic.

**Characteristics**:
- No downtime or service interruption
- Both old and new containers run in parallel (briefly)
- Requires successful health check validation before cutover
- Safer for production environments and SLA-critical services
- Requires score >= 70

**When Used**: 
- Stateless services without port conflicts
- Services with proper health checks
- High-availability deployments
- SLA-critical applications

**Example**:
```
Time 1: Old container running, receiving traffic
Time 2: New container starts with updated secrets
Time 3: New container passes health checks
Time 4: Traffic cut over to new container (graceful shutdown of old)
Time 5: Old container stopped, rotation complete
Total downtime: ~0 seconds
```

### 2. Restart Strategy (Brief Downtime)

**Definition**: Stops the old container completely, then starts a new container with updated secrets.

**Characteristics**:
- Brief service interruption (typically 5-30 seconds)
- Only one container running at a time
- Used when parallel execution is risky or impossible
- Simpler, more compatible with constrained environments
- Used when score < 70

**When Used**:
- Containers with fixed port bindings
- Containers with explicit names
- Containers with restart:always policy
- Stateful workloads or databases
- Any container with characteristics incompatible with parallel execution

**Example**:
```
Time 1: Old container running, receiving traffic
Time 2: Old container stopped (traffic interrupted)
Time 3: New container starts with updated secrets
Time 4: New container ready, traffic resumes
Total downtime: ~5-30 seconds (depending on startup time)
```

## Strategy Selection Algorithm

### Scoring System

DSO uses a point-based scoring system to evaluate suitability for rolling updates:

```
Base Score: 100 points
Final Score: 100 - (sum of all applicable penalties)

Selection:
  if (score >= 70) → Rolling Strategy
  else            → Restart Strategy
```

### Scoring Breakdown

| Factor | Penalty | Impact | Reason |
|--------|---------|--------|--------|
| **Fixed Port Binding** | -50 | Critical | Cannot run two containers on same fixed port |
| **Stateful Workload** | -20 | High | Risk of data corruption with parallel instances |
| **Explicit Container Name** | -20 | High | Docker enforces unique names; cannot scale to 2 |
| **Restart Always Policy** | -20 | High | restart:always may restart old container during rotation |
| **No Health Check** | -10 | Medium | Cannot validate new container is ready for cutover |

### Score Examples

```
Scenario 1: Stateless Web Service
  Base:                    100
  No issues:               100
  Score: 100 → ROLLING (suitable for zero-downtime)

Scenario 2: Service with Health Check + Fixed Port
  Base:                    100
  Fixed Port:              -50
  Health Check Present:     0 (no penalty)
  Score: 50 → RESTART (port conflict prevents parallel running)

Scenario 3: Database Container
  Base:                    100
  Fixed Port:              -50
  Stateful Workload:       -20
  Restart Always:          -20
  Explicit Container Name: -20
  No Health Check:         -10
  Score: -20 → RESTART (critical issues with parallel execution)

Scenario 4: Microservice without Health Check
  Base:                    100
  No Health Check:         -10
  Score: 90 → ROLLING (health check missing but other factors acceptable)
```

## Analysis Factors

### 1. Fixed Port Binding (-50 points)

**What It Means**: The container listens on a specific, fixed port (e.g., port 8080).

**Why It Matters**: 
- Docker requires port uniqueness on the same host
- Cannot run two containers both listening on port 8080
- Makes parallel execution (rolling strategy) impossible

**Detection**: Analyzed from container port configuration and PortBindings

**Mitigation**:
- Use port ranges or dynamic port binding
- Use service discovery/load balancing that doesn't rely on fixed ports
- Consider running on different hosts or using Kubernetes/Swarm

### 2. Explicit Container Name (-20 points)

**What It Means**: The container has an explicitly specified name (not auto-generated).

**Why It Matters**:
- Docker enforces container name uniqueness
- If you specify name "my-app", you cannot have two containers both named "my-app"
- Prevents scaling to multiple parallel instances

**Detection**: Container is created with explicit name (e.g., `docker run --name my-app`)

**Mitigation**:
- Allow Docker to auto-generate container names
- Use service orchestration (Kubernetes, Docker Swarm) for name management
- Use instance identifiers in name with scaling

### 3. Restart Always Policy (-20 points)

**What It Means**: The container has `restart: always` policy configured.

**Why It Matters**:
- Docker automatically restarts the container if it stops
- During rotation, the old container is stopped but immediately restarted
- Causes conflicts with the new container during the planned rotation
- Creates an uncontrolled situation during the cutover window

**Detection**: Container RestartPolicy is set to "always"

**Mitigation**:
- Change restart policy to `restart: unless-stopped` or `restart: on-failure`
- Implement your own restart logic with orchestration tools
- Use process managers (systemd, supervisor) with conditional restart rules

### 4. No Health Check (-10 points)

**What It Means**: The container doesn't define a HEALTHCHECK instruction or an equivalent mechanism.

**Why It Matters**:
- Without a health check, DSO cannot determine if the new container is ready
- Cannot validate the new container is functioning before cutting over traffic
- May cut over to a container that's still starting up or has failures
- Increases risk of introducing degraded service during rotation

**Detection**: Container has no HEALTHCHECK instruction and no health check probe configured

**Mitigation**:
- Add HEALTHCHECK to Dockerfile for the container
- Implement a /health or /ready endpoint in the application
- Use orchestration platform health checks (Kubernetes probes, etc.)
- Implement application-specific startup verification

### 5. Stateful Workload (-20 points)

**What It Means**: The container manages persistent data (databases, caches, file systems).

**Why It Matters**:
- Multiple instances accessing the same data causes race conditions
- Risk of data corruption, inconsistency, or loss
- Violates assumptions about single-instance operation
- State may be lost if old instance is stopped while new is starting

**Detection**: Analyzed based on:
- Presence of volume mounts
- Known stateful service detection
- Container image analysis (database services, cache systems)

**Mitigation**:
- Use dedicated data store (external database, managed service)
- Implement leader election for multi-instance scenarios
- Use distributed consensus protocols
- Ensure proper state migration strategies before rotation

## Implementation

### Code Location

The strategy decision logic is implemented in:
```
internal/strategy/decision_engine.go
```

**Key Components**:
- `StrategyDecision` struct: Holds the decision result
- `DecideStrategy()` function: Performs the analysis and scoring

### Data Flow

```
Container Analysis
        ↓
analyzer.AnalysisResult
        ↓
strategy.DecideStrategy()
        ↓
Scoring Algorithm Applied
        ↓
StrategyDecision Returned
        ↓
Rotation Engine Uses Selected Strategy
```

### Integration Points

DSO uses the strategy decision in:
1. **Rotation Orchestration**: Determines which executor to use
2. **User Reporting**: Explains why a specific strategy was selected
3. **Audit Logging**: Records analysis factors and scoring decisions
4. **Monitoring**: Tracks strategy usage patterns

## Example Output

### Analyzer Report (Input to Strategy Decision)

```
[DSO ANALYZER]
Container: my-web-app
- Fixed Port: NO
- Restart Policy: NO
- Stateful: NO
- Health Check: YES
```

### Strategy Decision Report (Output)

```
[DSO STRATEGY]
Selected: rolling
Score: 100
Reason:
- Stateless, highly available workload
```

### Combined Report

```
[DSO ANALYZER]
Container: my-web-app
- Fixed Port: NO
- Restart Policy: NO
- Stateful: NO
- Health Check: YES

[DSO STRATEGY]
Selected: rolling
Score: 100
Reason:
- Stateless, highly available workload
```

### Complex Example with Multiple Factors

```
[DSO ANALYZER]
Container: legacy-app
- Fixed Port: YES (8080)
- Restart Policy: ALWAYS
- Stateful: NO
- Health Check: NO

[DSO STRATEGY]
Selected: restart
Score: 50
Reason:
- Fixed port binding prevents parallel containers
- Restart policy conflicts with rotation engine
- Lack of health check prevents safe cutover validation
```

## Testing

### Unit Tests

Run strategy decision tests:
```bash
go test ./internal/strategy -v
```

### Test Scenarios Covered

1. **Ideal Case**: Stateless service with no issues → Rolling
2. **Fixed Port**: Port conflict prevents rolling → Restart
3. **Stateful**: Database/cache detected → Restart
4. **Missing Health Check**: Not critical but reduces score
5. **Multiple Penalties**: Cumulative scoring accuracy
6. **Edge Cases**: Score exactly at threshold (70)

### Manual Testing

To test strategy selection in a real environment:

```bash
# Analyze a specific container
dso analyze <container-id>

# View the strategy decision
dso rotate --dry-run <container-id>
```

## Decision Tree

```
Start
  ↓
Analyze Container
  ├─ Has Fixed Port? → Score -50
  ├─ Has Explicit Name? → Score -20
  ├─ Restart Always? → Score -20
  ├─ No Health Check? → Score -10
  └─ Stateful? → Score -20
  ↓
Calculate Final Score
  ↓
Score >= 70?
  ├─ YES → Select ROLLING Strategy
  └─ NO → Select RESTART Strategy
  ↓
Generate Report
  ↓
Return StrategyDecision
```

## Operational Considerations

### For Operators

**Understanding Restart Strategy Selection**:
- If DSO chooses restart for your container, address the underlying factors
- Each penalty factor can be mitigated independently
- Review the reason field to understand which factors triggered restart mode

**Improving Rotation Strategy**:
1. Add health checks to Dockerfile
2. Remove fixed port bindings where possible
3. Change restart policy from "always" to "unless-stopped"
4. Use auto-generated container names
5. Migrate stateful workloads to external datastores

### For Developers

**Building Rotation-Friendly Containers**:
- Include HEALTHCHECK in Dockerfile
- Avoid hardcoding container names
- Don't use restart:always (let orchestrator handle it)
- Avoid fixed port bindings when possible
- Keep containers stateless or use external data stores

### For SREs

**Monitoring Strategy Usage**:
- Track ratio of rolling vs restart rotations
- Investigate sudden shifts toward restart strategy
- Alert if high-value services shift to restart mode
- Use as input to infrastructure improvement initiatives

## Related Documentation

- `internal/strategy/decision_engine.go`: Implementation code
- `internal/analyzer/`: Container analysis engine
- Rotation executor documentation (rolling and restart strategies)
- Secret management workflow documentation
