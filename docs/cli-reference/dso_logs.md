## dso logs

View DSO agent logs

### Synopsis

View logs from the DSO Agent service.

By default, reads from the systemd journal (journald) if running as a service.
Use --api to stream live events from the agent REST API instead.

Examples:
  docker dso logs                          # Show last 100 lines
  docker dso logs -f                       # Follow live (tail -f style)
  docker dso logs -n 50                    # Show last 50 lines
  docker dso logs --since "10 minutes ago" # Logs from last 10 minutes
  docker dso logs --level error            # Filter to errors only
  docker dso logs --api                    # Stream from REST API

```
dso logs [flags]
```

### Options

```
      --api               Use the agent REST API instead of journald
      --api-addr string   Agent REST API address (used when journald unavailable) (default "http://localhost:8471")
  -f, --follow            Follow log output in real-time
  -h, --help              help for logs
      --level string      Filter by log level: debug, info, warn, error, fatal
      --since string      Show logs since timestamp or duration (e.g. '10 minutes ago', '2026-04-07 10:00:00')
  -n, --tail int          Number of lines to show from the end of the logs (default 100)
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

