## dso central backup

Write a verified, on-demand snapshot of the central database

### Synopsis

Writes a complete, self-contained, independently verified snapshot of
the central database to --out, using the same consistency mechanism (SQLite's
own VACUUM INTO) and verification (PRAGMA integrity_check plus a schema
version check) as the existing migration-time backup. It never overwrites an
existing file at --out.

The backup contains every table, including the full audit history. It never
contains the root key or passphrase: preserve that separately, or this backup
cannot be used to recover any secret value. Backups are never pruned
automatically; managing accumulated backup files is the operator's
responsibility.

```
dso central backup [flags]
```

### Options

```
  -h, --help         help for backup
      --out string   destination path for the backup (required; must not already exist)
```

### Options inherited from parent commands

```
  -c, --config string            config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --db string                path to the SQLite database (required)
      --key-file string          path to the root key file (required)
      --key-provider string      root key provider: file or passphrase (required)
      --no-color                 Disable ANSI colour in output (also honours NO_COLOR)
      --passphrase-file string   read the passphrase from this file (mode 0600) instead of prompting
```

### SEE ALSO

* [dso central](dso_central.md)	 - Local administration of the central secrets store (experimental, local-only, no authentication)

