## dso export

Export injected secrets for local CI or testing

```
dso export [flags]
```

### Options

```
  -f, --format string   Output format (env) (default "env")
  -h, --help            help for export
  -o, --output string   Output file destination (default ".env.local")
      --overwrite       Replace the output file if it already exists (default: refuse). The replacement is atomic and always mode 0600
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

