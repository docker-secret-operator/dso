## dso apply

Apply declarative secret configuration

### Synopsis

Apply DSO configuration from dso.yaml to running containers.

Similar to 'terraform apply', shows what will change before applying.
Only updates secrets that have actually changed (via checksum verification).

Examples:
  docker dso apply              # Show plan and wait for confirmation
  docker dso apply --dry-run    # Show what would change
  docker dso apply --force      # Apply without confirmation
  docker dso apply -c custom.yaml --timeout 60s

```
dso apply [flags]
```

### Options

```
      --dry-run            Show what would change without applying
      --force              Skip confirmation prompt
  -h, --help               help for apply
      --timeout duration   Reconciliation timeout (default 30s)
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

