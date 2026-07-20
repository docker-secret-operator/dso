# DSO End-to-End Workflow Review

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Conduct a comprehensive review of DSO's end-to-end workflow from core logic through setup, deployment, and working functionalities to identify gaps, inconsistencies, and issues.

**Architecture:** DSO is a single-host Docker secret injection daemon with multiple layers: CLI commands → setup/initialization → agent daemon → secret rotation engine → Docker integration. This review examines each layer's implementation, integration points, and adherence to spec.

**Scope:** Core agent logic, setup workflow (Detect→Validate→Plan→Apply), configuration loading, rotation strategies, provider system, CLI commands, REST API, and deployment model.

**Tech Stack:** Go 1.21+, Docker Compose, systemd integration, File-based state persistence, Multi-provider support (AWS/Azure/Vault/Local)

---

## Task 1: System Architecture & Entry Points

**Files:**
- Read: `cmd/dso/main.go`
- Read: `internal/cli/cli.go`
- Read: `internal/agent/agent.go`
- Read: `internal/server/server.go`
- Deliverable: `docs/ARCHITECTURE_REVIEW.md` (document architecture findings)

**Purpose:** Map the system's entry points, core flow, and integration boundaries.

- [ ] **Step 1: Read main CLI entry point**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/cmd/dso/main.go | head -100
```

Understand: How is the CLI initialized? What's the command dispatch structure?

- [ ] **Step 2: Review CLI package structure**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/cli/cli.go | head -150
```

Document: What are the top-level commands (setup, watch, status, doctor, repair)? How does each route to handlers?

- [ ] **Step 3: Review agent daemon entry**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/agent/agent.go | head -150
```

Document: What's the agent's initialization? What are the main loop responsibilities?

- [ ] **Step 4: Review server/API layer**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/server/server.go | head -100
```

Document: What endpoints does DSO expose? REST API or IPC?

- [ ] **Step 5: Create architecture diagram**

In `docs/ARCHITECTURE_REVIEW.md`, document:
- Entry points: CLI, agent daemon, REST API, IPC socket, Docker plugin socket
- Main components: CLI dispatcher, agent core, rotation engine, provider system, config loader
- Data flows: command → agent → provider → docker → rotation loop
- Integration points: Docker socket, systemd, config files, state persistence

Example structure for the document:
```markdown
# DSO Architecture Review

## Entry Points
- CLI commands via `cmd/dso`
- Daemon agent via systemd service
- REST API on :8471
- IPC socket at /run/dso/dso.sock
- Docker plugin at /run/docker/plugins/dso.sock

## Core Components
1. CLI Handler (internal/cli) - Command dispatch
2. Agent Daemon (internal/agent) - Main loop
3. Rotation Engine (internal/rotation) - Secret rotation logic
4. Provider System (internal/providers) - Multi-provider support
5. Config Loader (pkg/config) - YAML parsing & validation
6. Docker Integration (internal/compose) - Container management
7. TCP Proxy (internal/proxy) - Port binding management
8. Vault (pkg/vault) - Local encryption

## Data Flows
[Document the main flows: CLI→Setup, CLI→Watch, Agent polling loop, Rotation trigger]

## Known Integration Points
[List files that touch multiple components]
```

---

## Task 2: Setup Workflow (Detect → Validate → Plan → Apply)

**Files:**
- Read: `internal/setup/*.go`
- Read: `internal/analyzer/*.go`
- Read: `internal/bootstrap/*.go`
- Deliverable: `docs/SETUP_WORKFLOW_REVIEW.md`

**Purpose:** Verify the setup pipeline matches the spec and handles all cases correctly.

- [ ] **Step 1: Review setup entry point**

```bash
ls -la /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/setup/
```

- [ ] **Step 2: Map setup stages**

```bash
grep -r "func.*Detect\|func.*Validate\|func.*Plan\|func.*Apply" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/setup/ | head -20
```

Document each stage (Detect, Validate, Plan, Apply, Rollback). What does each do? What are failure modes?

- [ ] **Step 3: Check analyzer package**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/analyzer/analyzer.go | head -100
```

What environment detection logic exists? What cloud providers are detected?

- [ ] **Step 4: Check bootstrap logic**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/bootstrap/bootstrap.go | head -150
```

What happens during bootstrap? systemd integration? Group creation?

- [ ] **Step 5: Create setup workflow document**

In `docs/SETUP_WORKFLOW_REVIEW.md`:
```markdown
# Setup Workflow Review

## Expected Pipeline (from README)
Detect → Validate → Plan → Preview → Apply → Rollback (on failure)

## Actual Implementation
[Document what each stage does]

### Detect Stage
- Files: internal/setup/detect.go
- Responsibilities: [list]
- Cloud providers detected: [list]

### Validate Stage
- Files: internal/setup/validate.go
- Responsibilities: [list]
- Validation rules: [list]

### Plan Stage
- Files: internal/setup/plan.go
- Responsibilities: [list]

### Apply Stage
- Files: internal/setup/apply.go
- Responsibilities: [list]
- Side effects: [list]

### Rollback on Failure
[Document rollback mechanism]

## Gaps Found
[If any stages are missing or incomplete]

## Integration Points
- systemd service installation
- Group creation (dso group)
- Directory creation (/etc/dso, /run/dso)
- Config file generation
- Permission setup
```

---

## Task 3: Configuration Loading & Validation

**Files:**
- Read: `pkg/config/config.go`
- Read: `pkg/config/validation.go`
- Read: `pkg/schema/dso.json`
- Read: `examples/*.yaml`
- Deliverable: `docs/CONFIG_VALIDATION_REVIEW.md`

**Purpose:** Verify configuration is correctly parsed, validated, and applied.

- [ ] **Step 1: Review config structure**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/pkg/config/config.go | head -200
```

Document: What's the Config struct? What fields are required vs optional?

- [ ] **Step 2: Check validation rules**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/pkg/config/validation.go | head -200
```

Document: What validation happens? Required fields, type checks, provider validation?

- [ ] **Step 3: Verify JSON schema**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/pkg/schema/dso.json | head -100
```

Check: Is schema complete? Does it match the Go struct?

- [ ] **Step 4: Test against examples**

```bash
ls -la /data/umair_atr1123/All_Data/Antigravity_Work/dso/examples/*.yaml
```

For each example (local, aws, azure, vault): Parse it mentally with the config loader. Would it load successfully?

- [ ] **Step 5: Create validation review document**

In `docs/CONFIG_VALIDATION_REVIEW.md`:
```markdown
# Configuration Loading & Validation Review

## Config Structure (pkg/config/config.go)
[Describe the Config struct]

## Required vs Optional Fields
[Table showing field name, type, required/optional, validation rules]

## Validation Rules Applied
1. Version check
2. Mode validation (local/agent)
3. Provider validation (aws/azure/vault/local)
4. Secret mapping validation
5. Rotation strategy validation
6. Polling interval validation

## JSON Schema Coverage
- Complete? [Yes/No]
- Matches Go struct? [Yes/No]
- All required fields? [Yes/No]

## Test Cases Against Examples
- dso-local.yaml: [Pass/Fail]
- dso-aws.yaml: [Pass/Fail]
- dso-azure.yaml: [Pass/Fail]
- dso-vault.yaml: [Pass/Fail]

## Gaps/Issues Found
[List any missing validation, inconsistencies, or edge cases]
```

---

## Task 4: Core Rotation Logic & State Management

**Files:**
- Read: `internal/rotation/rotation.go`
- Read: `internal/rotation/executor.go`
- Read: `internal/agent/agent.go` (rotation loop)
- Read: `internal/core/*.go`
- Deliverable: `docs/ROTATION_LOGIC_REVIEW.md`

**Purpose:** Verify rotation logic is correct and handles all strategies.

- [ ] **Step 1: Review rotation entry point**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/rotation/rotation.go | head -150
```

Document: What triggers a rotation? What's the rotation pipeline?

- [ ] **Step 2: Check rotation strategies**

```bash
grep -r "rolling\|restart\|signal\|none" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/rotation/ | grep -i "strategy\|func" | head -20
```

Document each strategy (rolling, restart, signal, none). How is each implemented?

- [ ] **Step 3: Review executor**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/rotation/executor.go | head -200
```

Document: How are containers rotated? Health checks? Cleanup?

- [ ] **Step 4: Check state persistence**

```bash
grep -r "state\|persist\|save\|load" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/agent/ | grep -i "func\|type" | head -15
```

Document: What state is persisted? Where? How is it recovered on crash?

- [ ] **Step 5: Check lock mechanism**

```bash
grep -r "Lock\|Mutex\|lock" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/agent/ | head -10
```

Document: How does DSO prevent concurrent rotations?

- [ ] **Step 6: Create rotation review document**

In `docs/ROTATION_LOGIC_REVIEW.md`:
```markdown
# Rotation Logic & State Management Review

## Rotation Pipeline
1. Detect secret change (polling vs webhook)
2. Acquire distributed lock
3. Create new container with updated secret
4. Health check new container
5. Atomic swap (rename old → backup, new → active)
6. Route traffic via TCP proxy
7. Stop old container
8. Rollback on failure (auto-restore previous state)

## Rotation Strategies Implemented
- rolling: Blue-green swap, zero-downtime
- restart: Stop old, start new
- signal: Send SIGHUP to running container
- none: Update secret cache only

[Document how each strategy is implemented]

## State Persistence
- State location: [path]
- What's persisted: [list]
- Crash recovery: [description]

## Lock Mechanism
- Type: [file-based/distributed]
- Location: [path]
- Timeout: [duration]
- Concurrent rotation prevention: [mechanism]

## Health Checks
- Default timeout: [duration]
- Configurable: [Yes/No]
- Failure handling: [description]

## Gaps/Issues Found
[List any issues in rotation logic]
```

---

## Task 5: Provider System & Secret Retrieval

**Files:**
- Read: `internal/providers/*.go`
- Read: `pkg/provider/*.go`
- Read: `cmd/plugins/*.go` (each provider plugin)
- Deliverable: `docs/PROVIDER_SYSTEM_REVIEW.md`

**Purpose:** Verify each provider is correctly integrated and handles its protocol correctly.

- [ ] **Step 1: Review provider interface**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/pkg/provider/*.go | head -100
```

Document: What's the Provider interface? Required methods?

- [ ] **Step 2: List available providers**

```bash
ls -la /data/umair_atr1123/All_Data/Antigravity_Work/dso/cmd/plugins/
```

- [ ] **Step 3: Review AWS provider**

```bash
grep -r "aws" /data/umair_atr1123/All_Data/Antigravity_Work/dso/cmd/plugins/ -l
```

Document: How are AWS Secrets Manager credentials obtained? Auth method? Region handling?

- [ ] **Step 4: Review local provider**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/pkg/vault/vault.go | head -100
```

Document: How does local vault work? Encryption method? Master key storage?

- [ ] **Step 5: Review provider registry**

```bash
grep -r "Provider.*register\|RegisterProvider" /data/umair_atr1123/All_Data/Antigravity_Work/dso/ | head -10
```

Document: How are providers registered? How is the correct provider selected for a secret?

- [ ] **Step 6: Create provider system review document**

In `docs/PROVIDER_SYSTEM_REVIEW.md`:
```markdown
# Provider System & Secret Retrieval Review

## Provider Interface
[Describe required methods and their contracts]

## Implemented Providers
1. AWS Secrets Manager
   - Auth method: [IAM/token/other]
   - Region handling: [description]
   - Caching: [Yes/No]
   - Retry logic: [Yes/No]

2. Azure Key Vault
   - Auth method: [MSI/SP/other]
   - Vault URL: [how specified]
   - Caching: [Yes/No]
   
3. HashiCorp Vault
   - Auth method: [token/AppRole/other]
   - Address: [how specified]
   - Caching: [Yes/No]

4. Local Vault
   - Encryption: [AES-256-GCM]
   - Master key: [storage location]
   - Vault file: [storage location]
   - Key derivation: [method]

## Provider Selection Logic
[Document how provider is selected for a secret]

## Error Handling
- Network failures: [retry strategy]
- Auth failures: [handling]
- Secret not found: [handling]

## Gaps/Issues Found
[List any provider-specific issues]
```

---

## Task 6: Docker Integration & Container Lifecycle

**Files:**
- Read: `internal/compose/*.go`
- Read: `internal/injector/*.go`
- Read: `internal/proxy/*.go`
- Deliverable: `docs/DOCKER_INTEGRATION_REVIEW.md`

**Purpose:** Verify DSO correctly manages container lifecycle and integrates with Docker.

- [ ] **Step 1: Review Docker client initialization**

```bash
grep -r "docker.NewClient\|NewDockerClient" /data/umair_atr1123/All_Data/Antigravity_Work/dso/ | head -5
```

Document: How is Docker socket accessed? Permissions? Error handling?

- [ ] **Step 2: Review injector package**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/injector/injector.go | head -150
```

Document: How are secrets injected? Env vs file injection? tmpfs usage?

- [ ] **Step 3: Review TCP proxy**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/proxy/proxy.go | head -150
```

Document: How does TCP proxy work? Port binding? Traffic routing during rotation?

- [ ] **Step 4: Review container labeling**

```bash
grep -r "dso.reloader\|dso.secrets\|dso.update" /data/umair_atr1123/All_Data/Antigravity_Work/dso/ | head -10
```

Document: What labels does DSO expect? How are they used?

- [ ] **Step 5: Check docker-compose integration**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/compose/compose.go | head -150
```

Document: How are compose files handled? Label injection? Environment variable handling?

- [ ] **Step 6: Create Docker integration review document**

In `docs/DOCKER_INTEGRATION_REVIEW.md`:
```markdown
# Docker Integration & Container Lifecycle Review

## Docker Socket Access
- Socket location: /var/run/docker.sock
- Permissions: [check requirements]
- Error handling: [describe]

## Secret Injection Methods
1. File injection (dsofile://)
   - Storage: tmpfs in container
   - Visibility: Hidden from docker inspect
   - Delivery: Via stdin

2. Env injection (dso://)
   - Storage: Container environment
   - Visibility: Visible in docker inspect
   - Warning: Logged on startup

## TCP Proxy
- Purpose: Own host port bindings to prevent traffic interruption during rotation
- Ports: [dynamic, per container]
- Traffic routing: [mechanism]
- Cleanup: [when/how]

## Container Labels
Required:
- dso.reloader: "true"
- dso.secrets: "secret_name"
- dso.update.strategy: "rolling|restart|signal|none"
- dso.host_ports: "3306:3306" (if using proxy)

## Compose File Handling
- Label injection: [Yes/No]
- Label preservation: [how]
- Environment variable handling: [description]

## Container Lifecycle
1. Container start: [label detection]
2. Secret injection: [when/how]
3. Secret change detection: [mechanism]
4. Rotation trigger: [steps]
5. New container creation: [steps]
6. Health check: [mechanism]
7. Atomic swap: [mechanism]
8. Traffic routing: [mechanism]
9. Old container cleanup: [steps]

## Gaps/Issues Found
[List any Docker integration issues]
```

---

## Task 7: CLI Commands & Command Dispatch

**Files:**
- Read: `internal/cli/*.go`
- Read: `cmd/dso/main.go`
- Deliverable: `docs/CLI_COMMANDS_REVIEW.md`

**Purpose:** Verify all documented CLI commands are implemented and work as documented.

- [ ] **Step 1: List all CLI commands**

```bash
grep -r "func.*Cmd\|AddCommand\|RegisterCommand" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/cli/ | grep -v "test" | head -30
```

- [ ] **Step 2: Map commands to documentation**

For each command in README (setup, up, down, watch, status, doctor, repair, init, secret):
```bash
grep -n "docker dso <command>" /data/umair_atr1123/All_Data/Antigravity_Work/dso/README.md
```

- [ ] **Step 3: Check setup command**

```bash
grep -A 50 "func.*SetupCmd\|Setup command" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/cli/*.go | head -100
```

Document: setup flags, execution, integration with setup package

- [ ] **Step 4: Check watch command**

```bash
grep -A 30 "func.*WatchCmd\|Watch command" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/cli/*.go | head -60
```

Document: watch stream type, event filtering, output format

- [ ] **Step 5: Check status command**

```bash
grep -A 30 "func.*StatusCmd\|Status command" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/cli/*.go | head -60
```

Document: status output format, JSON support, watch mode

- [ ] **Step 6: Check doctor command**

```bash
grep -A 30 "func.*DoctorCmd\|Doctor command" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/cli/*.go | head -60
```

Document: number of checks, check categories, reporting format

- [ ] **Step 7: Check repair command**

```bash
grep -A 30 "func.*RepairCmd\|Repair command" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/cli/*.go | head -60
```

Document: fix categories, interactive prompts, dry-run support

- [ ] **Step 8: Create CLI commands review document**

In `docs/CLI_COMMANDS_REVIEW.md`:
```markdown
# CLI Commands Review

## Command Implementation Status

| Command | Documented | Implemented | Flags | Output Format |
|---------|-----------|-------------|-------|---------------|
| dso setup | Yes | [?] | --mode, --provider, --dry-run | Text |
| dso up | Yes | [?] | | |
| dso down | Yes | [?] | | |
| dso watch | Yes | [?] | --debug | Event stream |
| dso status | Yes | [?] | --json, --watch | Text/JSON |
| dso doctor | Yes | [?] | --level, --json | Text/JSON |
| dso repair | Yes | [?] | --dry-run | Text |
| dso init | Yes | [?] | | |
| dso secret set | Yes | [?] | | |
| dso system logs | Yes | [?] | -f, -p, --since, --api | Text |

## Command Details

### setup
- Flags: [list]
- Modes: local, agent
- Providers: aws, azure, vault
- Does it call analyzer → validator → planner → applier? [Yes/No]
- Rollback on failure? [Yes/No]

### watch
- Event types: [list from README]
- Actual events returned: [list]
- Filter support: [Yes/No]
- Raw payload option: [Yes/No]

### status
- Information returned: [list]
- JSON schema: [documented?]
- Watch mode: [Yes/No]

### doctor
- Documented checks: 17+
- Actual checks: [count]
- Check categories: [list]
- JSON output: [Yes/No]

### repair
- Documented fixes: safe/moderate/destructive
- Actual categorization: [verified?]
- Dry-run: [Yes/No]
- Prompts for destructive: [Yes/No]

## Gaps/Issues Found
[List any missing commands or incorrect implementations]
```

---

## Task 8: REST API Endpoints & IPC Interface

**Files:**
- Read: `internal/server/*.go`
- Read: `pkg/api/*.go`
- Deliverable: `docs/API_INTERFACE_REVIEW.md`

**Purpose:** Verify all API endpoints are correctly implemented and match their documented contracts.

- [ ] **Step 1: List all API endpoints**

```bash
grep -r "HandleFunc\|Route\|GET\|POST\|PUT\|DELETE" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/server/ | grep -v "test" | head -40
```

- [ ] **Step 2: Check IPC interface**

```bash
grep -r "IPC\|socket\|.sock" /data/umair_atr1123/All_Data/Antigravity_Work/dso/pkg/api/ | head -20
```

Document: How does CLI communicate with agent? IPC protocol? Message format?

- [ ] **Step 3: Review health endpoint**

```bash
grep -A 20 "health\|/health" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/server/*.go | head -40
```

Document: Response format, status codes

- [ ] **Step 4: Review metrics endpoint**

```bash
grep -A 20 "metrics\|/metrics\|prometheus" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/server/*.go | head -40
```

Document: Prometheus metrics format, exported metrics

- [ ] **Step 5: Review events endpoint**

```bash
grep -A 20 "events\|/events" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/server/*.go | head -40
```

Document: Event stream format, event types, filtering

- [ ] **Step 6: Create API review document**

In `docs/API_INTERFACE_REVIEW.md`:
```markdown
# REST API & IPC Interface Review

## Server Details
- REST API port: 127.0.0.1:8471
- IPC socket: /run/dso/dso.sock
- Docker plugin socket: /run/docker/plugins/dso.sock

## REST API Endpoints

| Method | Path | Purpose | Response |
|--------|------|---------|----------|
| GET | /health | Health check | {status: "ok"} |
| GET | /metrics | Prometheus metrics | Text format |
| GET | /events | Event stream | Server-sent events |
| GET | /status | Agent status | JSON |

[Document all actual endpoints]

## IPC Interface
- Protocol: [Unix domain socket]
- Message format: [JSON/protobuf/other]
- Request/response types: [list]
- Error format: [describe]

## Event Types
- Secret rotated: [format]
- Container created: [format]
- Container removed: [format]
- Rotation failed: [format]
[List all event types]

## Metrics Exported
[List Prometheus metrics]

## Response Schemas
[Document JSON response structures for key endpoints]

## Gaps/Issues Found
[List any undocumented endpoints or API inconsistencies]
```

---

## Task 9: Configuration File Management & Permissions

**Files:**
- Read: `internal/bootstrap/bootstrap.go` (directory/file creation)
- Read: `internal/setup/*.go` (config file generation)
- Verify: Actual config files at `/etc/dso/dso.yaml`, `/run/dso/`, etc.
- Deliverable: `docs/CONFIG_MANAGEMENT_REVIEW.md`

**Purpose:** Verify configuration files are correctly created with proper permissions and ownership.

- [ ] **Step 1: Check directory creation**

```bash
grep -r "mkdir\|0755\|0775\|chmod\|chown" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/bootstrap/ | head -30
```

Document: What directories are created? Permissions? Ownership?

- [ ] **Step 2: Check config file generation**

```bash
grep -r "WriteFile\|Create\|dso.yaml" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/setup/ | head -20
```

Document: How is dso.yaml generated? Permissions? Backup of existing?

- [ ] **Step 3: Verify systemd integration**

```bash
grep -r "systemd\|service\|dso-agent" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/bootstrap/ | head -20
```

Document: Does DSO create/manage systemd service unit files?

- [ ] **Step 4: Check group management**

```bash
grep -r "dso group\|groupadd\|usermod" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/setup/ | head -20
```

Document: Does DSO create the `dso` group? How are permissions managed?

- [ ] **Step 5: Check state directory**

```bash
ls -la /run/dso/ 2>/dev/null || echo "Directory does not exist"
ls -la /etc/dso/ 2>/dev/null || echo "Directory does not exist"
```

Document: Actual permissions if DSO is installed

- [ ] **Step 6: Create config management review document**

In `docs/CONFIG_MANAGEMENT_REVIEW.md`:
```markdown
# Configuration File Management & Permissions Review

## Directory Structure

| Path | Permissions | Owner | Purpose |
|------|-----------|-------|---------|
| /etc/dso/ | 0775 | root:dso | Config directory |
| /etc/dso/dso.yaml | 0664 | root:dso | Main config file |
| /run/dso/ | 0755 | root | Runtime directory |
| /run/dso/dso.sock | 0660 | root:dso | IPC socket |
| /var/lib/dso/ | 0755 | root | State directory |

[Verify actual values match spec]

## File Creation Process
1. Directory creation: [order and permissions]
2. Config file generation: [process]
3. Systemd unit file creation: [location and process]
4. Group creation: [process]
5. Permission assignment: [process]

## Group Management
- Group name: dso
- Group creation: [automatic? manual?]
- User addition: [automatic? documented?]
- Permissions granted: [describe]

## Config File Format
- Template: [if exists, describe]
- Generation logic: [describe]
- Validation after creation: [Yes/No]
- Backup of existing: [Yes/No, describe]

## systemd Integration
- Service name: dso-agent.service
- Service file location: /etc/systemd/system/dso-agent.service
- Auto-start: [Yes/No]
- Restart policy: [describe]

## Gaps/Issues Found
[List any permission issues, missing directories, or incorrect ownership]
```

---

## Task 10: Deployment Model & Systemd Integration

**Files:**
- Read: `internal/bootstrap/bootstrap.go`
- Read: `.github/workflows/*.yml` (CI/deployment)
- Verify: Installation script behavior
- Deliverable: `docs/DEPLOYMENT_REVIEW.md`

**Purpose:** Verify deployment process works correctly and can be automated.

- [ ] **Step 1: Review installation script**

```bash
head -100 /data/umair_atr1123/All_Data/Antigravity_Work/dso/scripts/install.sh 2>/dev/null || echo "Install script not found"
```

Document: What does install.sh do? Sudo requirements? Uninstall support?

- [ ] **Step 2: Check systemd service file**

```bash
ls -la /data/umair_atr1123/All_Data/Antigravity_Work/dso/.github/ 2>/dev/null
ls -la /data/umair_atr1123/All_Data/Antigravity_Work/dso/ | grep -i systemd
```

- [ ] **Step 3: Check CI/CD workflows**

```bash
ls -la /data/umair_atr1123/All_Data/Antigravity_Work/dso/.github/workflows/
```

Document: What CI runs? Testing, coverage, builds, releases?

- [ ] **Step 4: Check build configuration**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/Makefile
```

Document: Make targets, build process, testing

- [ ] **Step 5: Check release process**

```bash
cat /data/umair_atr1123/All_Data/Antigravity_Work/dso/.goreleaser.yml | head -50
```

Document: Release targets, binary outputs, artifacts

- [ ] **Step 6: Create deployment review document**

In `docs/DEPLOYMENT_REVIEW.md`:
```markdown
# Deployment Model & Systemd Integration Review

## Installation Modes

### Local Mode (user install)
- sudo required: No
- systemd: No
- Permissions: User-level
- Use case: Development
- Uninstall: [describe]

### Cloud/Agent Mode (system install)
- sudo required: Yes
- systemd: Yes
- Permissions: System-level
- Use case: Production
- Uninstall: [describe]

## Installation Process
1. Script: [location and what it does]
2. Binary placement: [path]
3. Directory creation: [order and process]
4. Group creation: [process]
5. systemd setup: [process]
6. Config generation: [process]

## systemd Service (dso-agent.service)
- ExecStart: [command]
- Restart: [policy]
- RestartSec: [duration]
- Type: [simple/forking/other]
- WorkingDirectory: [path]
- User/Group: [identity]
- Environment: [vars]

## Crash Recovery
- Mechanism: [systemd restart or built-in?]
- Recovery script: [if exists, describe]
- State restoration: [describe]

## Upgrade Process
- In-place: [Yes/No]
- Binary replacement: [steps]
- Config migration: [handled?]
- Data preservation: [Yes/No]
- Downtime: [zero/brief/other]

## CI/CD Pipeline
- Build targets: [list]
- Test coverage: [required % or check?]
- Release artifacts: [list]
- Deployment automation: [describe]

## Gaps/Issues Found
[List any deployment issues]
```

---

## Task 11: Testing Coverage & Test Strategy

**Files:**
- Verify: Test files exist (`*_test.go`)
- Read: Test configuration
- Deliverable: `docs/TESTING_REVIEW.md`

**Purpose:** Assess test coverage and identify gaps in testing.

- [ ] **Step 1: Count test files**

```bash
find /data/umair_atr1123/All_Data/Antigravity_Work/dso -name "*_test.go" -type f | wc -l
```

- [ ] **Step 2: Check coverage configuration**

```bash
grep -r "coverage\|codecov" /data/umair_atr1123/All_Data/Antigravity_Work/dso/.github/ 2>/dev/null | head -10
grep -r "coverage" /data/umair_atr1123/All_Data/Antigravity_Work/dso/Makefile 2>/dev/null
```

- [ ] **Step 3: Identify uncovered packages**

```bash
find /data/umair_atr1123/All_Data/Antigravity_Work/dso -path "*/internal/*" -name "*.go" ! -name "*_test.go" -type f | while read f; do
  base=$(dirname "$f")
  if [ ! -f "$base"/*_test.go ]; then
    echo "No tests: $f"
  fi
done | head -30
```

- [ ] **Step 4: Check integration tests**

```bash
grep -r "integration\|TestIntegration\|docker\|compose" /data/umair_atr1123/All_Data/Antigravity_Work/dso -l | grep test | head -20
```

- [ ] **Step 5: Check provider tests**

```bash
ls -la /data/umair_atr1123/All_Data/Antigravity_Work/dso/cmd/plugins/ | grep test
```

- [ ] **Step 6: Create testing review document**

In `docs/TESTING_REVIEW.md`:
```markdown
# Testing Coverage & Strategy Review

## Test File Count
- Total: [count]
- Unit tests: [count]
- Integration tests: [count]
- Coverage target: [percentage or documented?]

## Coverage by Package

| Package | Has Tests | Coverage |
|---------|----------|----------|
| internal/agent | [?] | [?]% |
| internal/rotation | [?] | [?]% |
| internal/setup | [?] | [?]% |
| internal/cli | [?] | [?]% |
| pkg/config | [?] | [?]% |
| pkg/vault | [?] | [?]% |
| internal/providers | [?] | [?]% |

## Test Categories

### Unit Tests
- Location: [describe pattern]
- Scope: [describe what they test]
- Mocking strategy: [describe]
- Table-driven: [Yes/No]

### Integration Tests
- Location: [describe]
- Docker usage: [Yes/No, how]
- Real providers: [which ones tested?]
- Test environments: [local/CI/both]

### Provider Tests
- AWS: [tested?]
- Azure: [tested?]
- Vault: [tested?]
- Local: [tested?]

## CI/CD Testing
- Unit test requirement: [Yes/No]
- Coverage requirement: [percentage or threshold]
- Integration tests in CI: [Yes/No]
- Failure blocks merge: [Yes/No]

## Gaps/Issues Found
[List packages or scenarios with insufficient test coverage]
```

---

## Task 12: Documentation Completeness & Accuracy

**Files:**
- Read: `docs/` directory
- Read: `README.md` (this is the spec)
- Deliverable: `docs/DOCUMENTATION_REVIEW.md`

**Purpose:** Verify documentation is complete, accurate, and matches implementation.

- [ ] **Step 1: List documentation files**

```bash
find /data/umair_atr1123/All_Data/Antigravity_Work/dso/docs -name "*.md" -type f
```

- [ ] **Step 2: Check for required docs**

```bash
for doc in "getting-started.md" "cli.md" "configuration.md" "architecture.md" "operational-guide.md" "security.md"; do
  [ -f "/data/umair_atr1123/All_Data/Antigravity_Work/dso/docs/$doc" ] && echo "✓ $doc" || echo "✗ Missing: $doc"
done
```

- [ ] **Step 3: Check README spec completeness**

Open README and verify:
- All CLI commands documented? [Yes/No]
- All config options documented? [Yes/No]
- All rotation strategies documented? [Yes/No]
- All providers documented? [Yes/No]
- All features documented? [Yes/No]

- [ ] **Step 4: Spot-check doc accuracy**

Pick 3 random documented features from README and verify they're actually implemented:
1. Feature: [name] - Implemented: [Yes/No]
2. Feature: [name] - Implemented: [Yes/No]
3. Feature: [name] - Implemented: [Yes/No]

- [ ] **Step 5: Check ADOPTERS.md**

```bash
wc -l /data/umair_atr1123/All_Data/Antigravity_Work/dso/ADOPTERS.md
```

- [ ] **Step 6: Create documentation review document**

In `docs/DOCUMENTATION_REVIEW.md`:
```markdown
# Documentation Completeness & Accuracy Review

## Required Documentation Files
| File | Exists | Completeness |
|------|--------|-------------|
| docs/getting-started.md | [?] | [?]% |
| docs/cli.md | [?] | [?]% |
| docs/configuration.md | [?] | [?]% |
| docs/architecture.md | [?] | [?]% |
| docs/operational-guide.md | [?] | [?]% |
| docs/providers.md | [?] | [?]% |
| SECURITY.md | [?] | [?]% |

## README Coverage

### CLI Commands
- setup: [documented]
- up: [documented]
- down: [documented]
- watch: [documented]
- status: [documented]
- doctor: [documented]
- repair: [documented]
[All documented?]

### Configuration Options
- All providers documented: [Yes/No]
- All rotation strategies documented: [Yes/No]
- All secret mapping options documented: [Yes/No]

### Providers
- AWS: [documented]
- Azure: [documented]
- Vault: [documented]
- Local: [documented]

### Features
- Zero-persistence: [documented]
- Rolling rotation: [documented]
- Multi-provider: [documented]
- Non-root operation: [documented]
- Deterministic rollback: [documented]
- TCP proxy: [documented]
- Crash recovery: [documented]
- Transactional setup: [documented]
- Doctor & repair: [documented]

## Accuracy Spot-Check
- Feature 1: documented vs implemented [match?]
- Feature 2: documented vs implemented [match?]
- Feature 3: documented vs implemented [match?]

## Examples
- examples/dso-local.yaml: [present and valid?]
- examples/dso-aws.yaml: [present and valid?]
- examples/dso-azure.yaml: [present and valid?]
- examples/dso-vault.yaml: [present and valid?]

## Gaps/Issues Found
[List any missing or inaccurate documentation]
```

---

## Task 13: Integration Points & Cross-Component Review

**Files:**
- All components (context provided from previous tasks)
- Deliverable: `docs/INTEGRATION_REVIEW.md`

**Purpose:** Identify integration gaps, missing error handling, and inconsistencies across components.

- [ ] **Step 1: Review component dependencies**

```bash
grep -r "import.*internal\|import.*pkg" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/ | cut -d: -f2 | sort -u | head -40
```

Document: What components depend on each other?

- [ ] **Step 2: Check error propagation**

```bash
grep -r "panic\|log.Fatal\|os.Exit" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/ | head -20
```

Document: How are errors handled? Proper error propagation? Panics?

- [ ] **Step 3: Check logging consistency**

```bash
grep -r "log\|logger\|Logger" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/ | grep "import\|Logger\|log.New" | head -20
```

Document: Logging strategy, log levels, structured logging?

- [ ] **Step 4: Check context usage**

```bash
grep -r "context.Context\|ctx.*Done\|ctx.*Cancel" /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/ | head -20
```

Document: Is context properly threaded? Cancellation handling?

- [ ] **Step 5: Check state synchronization**

```bash
grep -r "Mutex\|Lock\|RWMutex\|sync\." /data/umair_atr1123/All_Data/Antigravity_Work/dso/internal/ | head -20
```

Document: Concurrency safety, lock usage, race condition risks?

- [ ] **Step 6: Create integration review document**

In `docs/INTEGRATION_REVIEW.md`:
```markdown
# Integration Points & Cross-Component Review

## Component Dependencies

### CLI (internal/cli)
- Depends on: agent, setup, server
- Used by: main, docker plugin

### Agent (internal/agent)
- Depends on: rotation, providers, config, docker integration
- Used by: systemd, IPC interface

### Rotation (internal/rotation)
- Depends on: docker, injector, proxy, providers
- Used by: agent

### Setup (internal/setup)
- Depends on: analyzer, bootstrap, config
- Used by: CLI

## Error Handling
- Panic usage: [count and reasons]
- Fatal exits: [where and why]
- Error propagation: [strategy]
- User-facing errors: [documented?]

## Logging Strategy
- Logger type: [stdlib/structured/other]
- Log levels used: [list]
- Structured fields: [what's logged]
- Log rotation: [Yes/No]
- Secret redaction: [Yes/No, how?]

## Context Usage
- Thread context through calls: [Yes/No]
- Cancellation handling: [Yes/No]
- Timeout implementation: [describe]
- Goroutine lifecycle: [managed via context?]

## Concurrency Safety
- Mutex usage: [describe protected resources]
- Race condition risks: [list if any]
- Atomic operations: [where used]
- Channel communication: [where used]

## Missing Error Handling
[List scenarios where error handling seems inadequate]

## Gaps/Issues Found
[List integration gaps, missing validations, race conditions]
```

---

## Task 14: Summary & Findings Consolidation

**Deliverable:** `docs/END_TO_END_REVIEW_SUMMARY.md`

**Purpose:** Consolidate all findings into a comprehensive summary with actionable items.

- [ ] **Step 1: Create summary document**

In `docs/END_TO_END_REVIEW_SUMMARY.md`:

```markdown
# DSO End-to-End Workflow Review — Summary

**Date:** 2026-07-20  
**Reviewer:** [name]  
**Scope:** Core logic, setup, deployment, working functionalities, testing, and documentation

---

## Executive Summary
[2-3 sentence overview of review findings]

---

## Review Coverage

### Completed Tasks
- [x] Task 1: System Architecture & Entry Points
- [x] Task 2: Setup Workflow
- [x] Task 3: Configuration Loading & Validation
- [x] Task 4: Rotation Logic & State Management
- [x] Task 5: Provider System
- [x] Task 6: Docker Integration
- [x] Task 7: CLI Commands
- [x] Task 8: REST API & IPC Interface
- [x] Task 9: Configuration File Management
- [x] Task 10: Deployment Model
- [x] Task 11: Testing Coverage
- [x] Task 12: Documentation
- [x] Task 13: Integration Points

### Individual Review Documents
- `docs/ARCHITECTURE_REVIEW.md`
- `docs/SETUP_WORKFLOW_REVIEW.md`
- `docs/CONFIG_VALIDATION_REVIEW.md`
- `docs/ROTATION_LOGIC_REVIEW.md`
- `docs/PROVIDER_SYSTEM_REVIEW.md`
- `docs/DOCKER_INTEGRATION_REVIEW.md`
- `docs/CLI_COMMANDS_REVIEW.md`
- `docs/API_INTERFACE_REVIEW.md`
- `docs/CONFIG_MANAGEMENT_REVIEW.md`
- `docs/DEPLOYMENT_REVIEW.md`
- `docs/TESTING_REVIEW.md`
- `docs/DOCUMENTATION_REVIEW.md`
- `docs/INTEGRATION_REVIEW.md`

---

## Critical Findings

### High Priority (blocks production use)
[List 0-3 items]

### Medium Priority (should fix before next release)
[List 0-5 items]

### Low Priority (nice to have)
[List 0-3 items]

---

## Category Summaries

### Architecture
[Summary of findings from ARCHITECTURE_REVIEW.md]

### Setup Workflow
[Summary of findings from SETUP_WORKFLOW_REVIEW.md]

### Configuration
[Summary of findings from CONFIG_VALIDATION_REVIEW.md]

### Rotation Logic
[Summary of findings from ROTATION_LOGIC_REVIEW.md]

### Providers
[Summary of findings from PROVIDER_SYSTEM_REVIEW.md]

### Docker Integration
[Summary of findings from DOCKER_INTEGRATION_REVIEW.md]

### CLI Commands
[Summary of findings from CLI_COMMANDS_REVIEW.md]

### REST API & IPC
[Summary of findings from API_INTERFACE_REVIEW.md]

### Configuration Files & Permissions
[Summary of findings from CONFIG_MANAGEMENT_REVIEW.md]

### Deployment & systemd
[Summary of findings from DEPLOYMENT_REVIEW.md]

### Testing
[Summary of findings from TESTING_REVIEW.md]

### Documentation
[Summary of findings from DOCUMENTATION_REVIEW.md]

### Integration Points
[Summary of findings from INTEGRATION_REVIEW.md]

---

## Actionable Recommendations

### For Core Logic
1. [Recommendation with rationale]
2. [Recommendation with rationale]

### For Setup & Deployment
1. [Recommendation with rationale]
2. [Recommendation with rationale]

### For Testing
1. [Recommendation with rationale]
2. [Recommendation with rationale]

### For Documentation
1. [Recommendation with rationale]
2. [Recommendation with rationale]

---

## Next Steps
1. [Priority 1 action]
2. [Priority 2 action]
3. [Priority 3 action]

---

## Review Quality Notes
- Code inspections: Completed
- Documentation spot-checks: Completed
- Integration testing: [needed?]
- Live deployment verification: [needed?]
```

- [ ] **Step 2: Cross-reference findings**

Review all 13 individual review documents. For each finding, note:
- Category (Architecture/Setup/Config/Rotation/etc.)
- Priority (High/Medium/Low)
- Impact (Correctness/Security/Maintainability/Performance)
- Recommendation

- [ ] **Step 3: Group by theme**

Organize findings into themes:
- Missing implementations
- Inconsistencies with documentation
- Error handling gaps
- Testing gaps
- Performance issues
- Security concerns
- Configuration issues
- Integration problems

- [ ] **Step 4: Identify cross-component issues**

Look for issues that span multiple components:
- Error propagation failures
- State synchronization problems
- Configuration validation gaps
- API contract mismatches

- [ ] **Step 5: Finalize summary**

Complete the summary document with:
- All critical findings
- Prioritized recommendations
- Clear next steps

- [ ] **Step 6: Commit all review documents**

```bash
cd /data/umair_atr1123/All_Data/Antigravity_Work/dso
git add docs/ARCHITECTURE_REVIEW.md docs/SETUP_WORKFLOW_REVIEW.md docs/CONFIG_VALIDATION_REVIEW.md docs/ROTATION_LOGIC_REVIEW.md docs/PROVIDER_SYSTEM_REVIEW.md docs/DOCKER_INTEGRATION_REVIEW.md docs/CLI_COMMANDS_REVIEW.md docs/API_INTERFACE_REVIEW.md docs/CONFIG_MANAGEMENT_REVIEW.md docs/DEPLOYMENT_REVIEW.md docs/TESTING_REVIEW.md docs/DOCUMENTATION_REVIEW.md docs/INTEGRATION_REVIEW.md docs/END_TO_END_REVIEW_SUMMARY.md
git commit -m "docs: add comprehensive end-to-end workflow review findings"
```

---

## Review Methodology

This plan uses a **systematic layer-by-layer review** approach:

1. **Architecture layer** — Understand system boundaries and entry points
2. **Core logic layer** — Review business logic (setup, rotation, providers)
3. **Integration layer** — Check how components interact
4. **API layer** — Verify interfaces and contracts
5. **Configuration layer** — Review configuration handling and validation
6. **Deployment layer** — Check installation and systemd integration
7. **Testing layer** — Assess coverage and quality
8. **Documentation layer** — Verify completeness and accuracy
9. **Cross-cutting concerns** — Check error handling, logging, concurrency
10. **Consolidation** — Synthesize findings and create actionable recommendations

Each task produces a focused review document that can be reviewed independently or as part of the whole.

---

## Notes for Reviewers

- Read the README carefully — it's the spec. Note any contradictions with implementation.
- Check git blame for recent changes in areas you're reviewing.
- Look for TODO comments — they indicate known issues.
- Check test files for clues about intended behavior.
- Verify both happy paths and error cases.
- Look for edge cases that might not be handled.
- Check if configuration is validated both at load time and when used.
- Verify state is persisted and recovered correctly on crash.
- Ensure errors are properly propagated to the user.
- Check for race conditions in concurrent code.
