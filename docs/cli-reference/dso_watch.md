## dso watch

Real-time monitor of secret rotations and container lifecycles

### Synopsis

Stream Docker container events and, if the DSO agent is reachable, secret
rotation events, until interrupted (Ctrl+C).

Read-only: it does not change containers or secrets. Requires access to the
Docker daemon; agent events additionally need the agent socket
(DSO_SOCKET_PATH, default /run/dso/dso.sock). If the agent is not running,
a warning is shown and only Docker events appear.

```
dso watch [flags]
```

### Options

```
  -d, --debug             Enable raw event payload output
  -h, --help              help for watch
      --strategy string   Rotation strategy (auto/rolling/restart) (default "auto")
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

