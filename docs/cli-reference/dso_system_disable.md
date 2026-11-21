## dso system disable

Disable DSO agent service

### Synopsis

Disable and stop the DSO agent systemd service.

The service will no longer start automatically on boot.

Examples:
  sudo docker dso system disable              # Disable and stop agent

```
dso system disable [flags]
```

### Options

```
  -h, --help   help for disable
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso system](dso_system.md)	 - System-level DSO management

