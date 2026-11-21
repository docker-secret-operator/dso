## dso system restart

Restart DSO agent service

### Synopsis

Restart the DSO agent systemd service.

Useful for applying configuration changes or recovering from errors.

Examples:
  sudo docker dso system restart              # Restart agent service

```
dso system restart [flags]
```

### Options

```
  -h, --help   help for restart
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso system](dso_system.md)	 - System-level DSO management

