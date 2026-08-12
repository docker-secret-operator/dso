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

```
dso status [flags]
```

### Options

```
  -h, --help    help for status
      --json    Output as JSON
      --watch   Auto-refresh every 2 seconds
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

