## dso system logs

View DSO agent service logs

### Synopsis

Display DSO agent service logs from the systemd journal.

Use -f/--follow to monitor logs in real-time.
Use -n/--lines to control how many log lines to display.
Use -p/--priority to filter by log level (alert, crit, err, warning, notice, info, debug).

Examples:
  docker dso system logs                      # Show recent logs
  docker dso system logs -f                   # Follow logs in real-time
  docker dso system logs -n 50                # Show last 50 lines
  docker dso system logs -p err               # Show only errors

```
dso system logs [flags]
```

### Options

```
  -f, --follow            Follow logs in real-time (Ctrl+C to exit)
  -h, --help              help for logs
  -n, --lines int         Number of log lines to display (default 20)
  -p, --priority string   Filter by priority (alert, crit, err, warning, notice, info, debug)
  -S, --since string      Show logs since (e.g., '1h', '30m', '1h 30m')
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso system](dso_system.md)	 - System-level DSO management

