## dso status

Show DSO runtime operational status

### Synopsis

Display DSO runtime status including mode, providers, cache, and rotation health.

Provides operational visibility into the DSO system by querying the running
agent (via 'docker dso agent'/systemd) for its real, current state.

Examples:
  docker dso status              # Single status check
  docker dso status --watch      # Auto-refresh every 2 seconds
  docker dso status --json       # Machine-readable output

Exit status: 0 when the agent is reachable and healthy; 1 when the agent is
unreachable or reports degraded providers/services. Uptime is shown only when
it can be verified against a reachable agent; otherwise it is "unavailable".

```
dso status [flags]
```

### Options

```
  -h, --help    help for status
      --json    Output as JSON
  -q, --quiet   Print nothing; report health only through the exit status
      --watch   Auto-refresh every 2 seconds
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

