## dso agent

Run the DSO background reconciliation engine

### Synopsis

The agent command starts the DSO reconciliation loop, Unix socket server, and Docker Secret Driver interface.

```
dso agent [flags]
```

### Options

```
      --api-addr string        Address to bind the REST API server (default "127.0.0.1:8471")
      --driver-socket string   Path to Docker Secret Driver socket (default "/run/docker/plugins/dso.sock")
  -h, --help                   help for agent
      --metrics-addr string    Address to bind the Prometheus metrics server (default "127.0.0.1:9090")
      --socket string          Path to DSO internal IPC socket (default "/run/dso/dso.sock")
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

