# CLI Commands Review

**Date:** 2026-07-20  
**Scope:** CLI command implementation status and verification

---

## Command Implementation Status

Commands defined in `internal/cli/root.go` NewRootCmd():

| Command | File | Documented | Status | Purpose |
|---------|------|-----------|--------|---------|
| setup | setup.go | ✅ Yes | ✓ Impl | Interactive setup wizard |
| bootstrap | bootstrap.go | ✅ Yes | ✓ Impl | Manual bootstrap (low-level) |
| doctor | doctor.go | ✅ Yes | ✓ Impl | 17+ diagnostic checks |
| status | status.go | ✅ Yes | ✓ Impl | Operational status |
| config | - | ✅ Partial | ? | Config management |
| system | system_mgmt.go | ✅ Yes | ✓ Impl | System operations |
| agent | agent.go | ✅ Yes | ✓ Impl | Run daemon |
| metadata | metadata.go | ? | ✓ Impl | Metadata operations |
| compose | compose.go | ✅ Yes | ✓ Impl | Compose integration |
| fetch | fetch.go | ✅ Yes | ✓ Impl | Fetch secrets |
| init | - | ? | ✓ Impl | Initialize vault |
| apply | apply.go | ? | ✓ Impl | Apply config changes |
| inject | inject.go | ? | ✓ Impl | Inject secrets |
| sync | sync.go | ? | ✓ Impl | Sync operations |
| up | up.go | ✅ Yes | ✓ Impl | Start containers |
| down | down.go | ✅ Yes | ✓ Impl | Stop containers |
| watch | watch.go | ✅ Yes | ✓ Impl | Stream events |
| version | - | ✅ Yes | ✓ Impl | Show version |
| validate | validate.go | ? | ✓ Impl | Validate config |
| export | export.go | ? | ✓ Impl | Export secrets |
| inspect | inspect.go | ✅ Yes | ✓ Impl | Inspect container/state |
| diff | diff.go | ✅ Yes | ✓ Impl | Show changes |
| logs | logs.go | ✅ Yes | ✓ Impl | View agent logs |
| secret | secret.go | ✅ Yes | ✓ Impl | Secret management |

**Total**: 23 commands (all appear to have implementations)

---

## Command Details

### setup
- **File**: `internal/cli/setup.go`
- **Flags**: --mode (local/agent), --provider (aws/azure/vault), --dry-run
- **Integration**: Calls setup package pipeline (detect → validate → plan → apply)
- **Output**: Status messages, final summary
- **Rollback**: Yes (on failure)
- **Documented**: README shows example usage

### bootstrap (Low-level alternative to setup)
- **File**: `internal/cli/bootstrap.go`
- **Flags**: Mode selection, manual operation
- **Purpose**: For users who want to skip setup wizard
- **Documented**: Mentioned in README

### doctor (Health diagnostics)
- **File**: `internal/cli/doctor.go`
- **Checks**: README claims 17+
- **Categories** (likely):
  - Docker connectivity
  - DSO installation
  - Permissions
  - Config validity
  - Provider auth
  - Systemd (if agent mode)
- **Output**: Table with check name → pass/warn/fail
- **Flags**: --level (quick/full), --json (machine-readable)
- **Documented**: README shows usage

### status
- **File**: `internal/cli/status.go`
- **Information returned**:
  - Agent running? (yes/no)
  - Mode (local/agent)
  - Managed containers count
  - Recent rotations
  - Error status
- **Flags**: --json (JSON format), --watch (continuous polling)
- **Polling interval**: Probably 2-5 seconds
- **Documented**: README shows examples

### agent
- **File**: `internal/cli/agent.go`
- **Purpose**: Run DSO daemon
- **Usage**: `docker dso agent` or systemd service
- **Signals**: SIGTERM/SIGINT for graceful shutdown
- **Logging**: Uses zap logger (configured via flags/env)
- **Not documented in README**: (systemd handles this)

### watch
- **File**: `internal/cli/watch.go`
- **Purpose**: Stream real-time events
- **Events**: Container lifecycle, rotations, errors
- **Flags**: --debug (raw payloads?)
- **Output**: JSON or text format, one per line
- **Documented**: README shows `docker dso watch` example

### up / down
- **Files**: `internal/cli/up.go`, `internal/cli/down.go`
- **Purpose**: Start/stop containers (via compose)
- **Labels**: Injects DSO labels if not present
- **Integration**: Calls Docker Compose API
- **Documented**: README shows usage

### logs
- **File**: `internal/cli/logs.go`
- **Purpose**: View agent logs (via journald or REST API)
- **Flags**: -f (follow), -p (priority/level), --since (time range)
- **Fallback**: Can use REST API if journald unavailable
- **Documented**: README shows `docker dso system logs` (system is parent command)

### secret
- **File**: `internal/cli/secret.go`
- **Subcommands** (likely):
  - `set` - Store secret in vault
  - `get` - Retrieve secret value
  - `list` - List all secrets
  - `delete` - Remove secret
- **Documented**: README shows `docker dso secret set` example

### system
- **File**: `internal/cli/system_mgmt.go`
- **Subcommands** (likely):
  - `logs` - View logs
  - `status` - Service status
  - `enable` - Enable autostart
  - `disable` - Disable autostart
  - `restart` - Restart service
- **Documented**: Partially in README

### config
- **Likely subcommands**:
  - `show` - Display current config
  - `validate` - Check config validity
  - `edit` - Edit config (interactive?)
- **Documented**: README shows `docker dso config show` example

---

## Flags & Options Summary

### Global Flags (from root command)
- `-c` / `--config` - Config file path (default: /etc/dso/dso.yaml or ./dso.yaml)

### Command-Specific Flags
- `setup`: --mode, --provider, --dry-run
- `status`: --json, --watch
- `doctor`: --level, --json
- `logs`: -f, -p, --since, --api
- `watch`: --debug (?)
- `config`: (subcommand-specific)

### Likely Missing Flags
- Verbose/debug logging for all commands
- Timeout configurations
- Output format options (JSON, YAML, text)

---

## Gaps/Issues Found

### Verification Gaps:

1. **❓ Underdocumented commands**
   - init, apply, inject, sync, validate, export, metadata
   - Not mentioned in README
   - Purpose unclear

2. **❓ Subcommand structure unclear**
   - Are "system" commands nested? (dso system logs)
   - Are "config" commands nested? (dso config show)
   - Are "secret" commands nested? (dso secret set)

3. **❓ Flag consistency**
   - Do all commands support --help?
   - Do all support config file override (-c)?
   - Which commands support --json?
   - Which support --dry-run?

### Documentation Issues:

4. **❌ README incomplete**
   - Shows example usage but not all commands
   - Doesn't show all flags
   - No command reference (auto-generated?)

5. **❌ Nested command structure not documented**
   - Example: `dso system logs` or `dso logs`?
   - Example: `dso config show` or `dso show config`?
   - Consistency across all commands?

6. **❓ Subcommand naming inconsistent**
   - Some are verbs (setup, status, doctor)
   - Some are nouns (agent, system, config)
   - Some are paths (system logs, secret set)

### Functional Gaps:

7. **❓ Error handling unclear**
   - What exit codes are used? (0 = success, 1 = error?)
   - How are errors reported? (stderr?)
   - Are error messages user-friendly?

8. **❓ Configuration conflict resolution**
   - What if config file and CLI flags conflict?
   - Which takes precedence? (probably CLI)
   - Clear error if conflict? (or silent override?)

9. **❓ Shell completion**
   - Are bash/zsh completions provided?
   - Completion for subcommands?
   - Completion for flag values?

---

## Testing Gaps

### What Should Be Tested:
1. ✓ Each command executes without error
2. ✓ Help text for all commands (dso <cmd> --help)
3. ✓ Flag parsing for all documented flags
4. ✓ Config file loading and override via -c
5. ✓ Output format (text, JSON where applicable)
6. ✓ Error handling (invalid arguments, missing files)
7. ✓ Exit codes (0 for success, non-zero for errors)
8. ✓ Graceful shutdown (SIGTERM handling for daemon)

### Integration Tests:
- Setup → status flow
- Secret management (set → get → list → delete)
- Watch → event streaming
- Logs → log output

---

## Recommendations

1. **Document all commands** - Create CLI reference guide
2. **Clarify subcommand structure** - Show hierarchy (dso system logs or dso logs?)
3. **Document flags globally** - Which flags work with which commands?
4. **Add shell completion** - Bash/zsh completion scripts
5. **Standardize error messages** - Consistent format and exit codes
6. **Add --version to all commands** - Show version info
7. **Test all commands** - Ensure each works as documented
8. **Document environment variables** - DSO_CONFIG, etc.
9. **Show config file precedence** - Where it looks for config
10. **Add examples in help text** - `dso setup --help` should show example
