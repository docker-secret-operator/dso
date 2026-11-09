## dso down

Stop and remove the stack's containers and networks (volumes and images are kept by default)

### Synopsis

Runs 'docker compose down' with the given arguments.

By default this removes the stack's containers and networks ONLY. Named volumes
and images are kept. Destructive options are passed through to docker compose:
  -v, --volumes   also remove named volumes declared in the compose file (DATA LOSS)
  --rmi all|local also remove images
  --remove-orphans remove containers for services not in the compose file

Arguments are forwarded unchanged; DSO does not add or imply any of them.

```
dso down [args...] [flags]
```

### Options

```
  -h, --help   help for down
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

