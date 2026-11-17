## dso audit export

Export the agent's durable audit file as verified NDJSON (read-only)

### Synopsis

Verifies the whole hash chain of the agent's durable audit file and, only if it
verifies, writes every record as one JSON object per line (NDJSON), the same
format as GET /api/audit/export. Nothing is written to stdout on failure, so a
broken or unreadable file can never produce an export that looks valid. The
audit file itself is never modified.

By default the export goes to stdout. With --output FILE it is written to a new
file (mode 0600); an existing file is never overwritten.

Exit status is the same as `dso audit verify`: 10 integrity failure, 11 unavailable.

The chain is unkeyed and the audit file is best effort; an export is not proof
of authenticity or completeness (see `dso audit verify --help`).

```
dso audit export [flags]
```

### Options

```
      --file string     audit file to export (default: the agent's durable audit file)
  -h, --help            help for export
      --output string   write to this new file (mode 0600) instead of stdout
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso audit](dso_audit.md)	 - Inspect the agent's durable audit file

