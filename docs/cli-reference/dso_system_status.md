## dso system status

Show DSO agent service status

### Synopsis

Display the current status of the DSO agent systemd service.

Shows whether the service is running, enabled, and any recent errors.

Examples:
  docker dso system status              # Show agent service status

```
dso system status [flags]
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso system](dso_system.md)	 - System-level DSO management

