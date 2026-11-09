## dso diff

Not implemented: compare local configuration with a deployed stack

### Synopsis

Not implemented. This command always exits 1.

It is reserved for comparing the provider mappings in dso.yaml with a deployed
stack. A real comparison needs a defined source of truth for the deployed side
(container labels written by 'dso up', or the agent's tracked targets) and a
stable identity for "the stack" (project/environment selection), neither of
which the CLI exposes yet. Until then it reports that no comparison was made
rather than claiming "no differences".

Alternatives today:
  docker dso validate       # check the project and configuration
  docker dso inspect <id>   # show injected environment/mounts of one container
  docker dso config show    # view the (redacted) configuration

```
dso diff [stack-name] [flags]
```

### Options

```
  -h, --help   help for diff
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

