## dso repair

Repair DSO host state that diagnostics reported as wrong

### Synopsis

Repair groups explicit, narrowly scoped corrections for host state that
'dso doctor' can report. Nothing here runs automatically.

```
dso repair [flags]
```

### Options

```
  -h, --help   help for repair
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose
* [dso repair permissions](dso_repair_permissions.md)	 - Inspect and repair the DSO directory permission contract

