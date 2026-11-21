## dso audit

Inspect the agent's durable audit file

```
dso audit [flags]
```

### Options

```
  -h, --help   help for audit
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose
* [dso audit export](dso_audit_export.md)	 - Export the agent's durable audit file as verified NDJSON (read-only)
* [dso audit verify](dso_audit_verify.md)	 - Verify the agent's durable audit file hash chain (read-only)

