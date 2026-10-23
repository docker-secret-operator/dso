## dso up

Deploy a stack and automatically start the DSO agent

### Synopsis

The 'up' command is the primary entrypoint for DSO. It performs a Docker Compose deployment with secure secret injection.

DSO defaults to Cloud Mode (requires systemd/root configuration). Local Mode is available as an optional developer add-on by running 'docker dso init' or passing '--mode=local'.

```
dso up [args...] [flags]
```

### Options

```
  -h, --help   help for up
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

