## dso system enable

Enable DSO agent service

### Synopsis

Enable and start the DSO agent systemd service.

The service will start automatically on boot and restart on failure.

Examples:
  sudo docker dso system enable              # Enable and start agent

```
dso system enable [flags]
```

### Options

```
  -h, --help   help for enable
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso system](dso_system.md)	 - System-level DSO management

