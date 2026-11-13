## dso central recovery-status

Report recovery-related artifacts left beside the database, without changing anything

### Synopsis

Looks beside --db for the on-disk leftovers a raw preservation or a Restore
displacement can leave: a raw preservation directory ("<db>.pre-restore-raw-*",
published or still ".partial"), a displacement aside directory
("<db>.replaced-*"), and a prepared-replacement temporary file
("<db>.restoring-*"). It reports, for each one, the filesystem facts the
scanner can prove and the specific questions it cannot answer -- it never
claims an artifact is safe to delete or safe to restore, never infers what
happened from a filename, timestamp or directory shape alone, and never
treats age alone as proof of anything.

This command does not require --db to exist: a crash can leave the active
database missing entirely, which is one of the states worth reporting.

Before reading the filesystem it attempts the shared central lock, the same
one every other dso central command takes. This advisory lock coordinates
dso central processes only -- it does not exclude an external SQLite process
or another host. If acquisition succeeds, it scans and reports the recovery
artifacts it finds. If acquisition fails (for example because an exclusive
Restore is in progress), scanning and classification are NOT performed: no
filesystem fact is read or reported for that run, and the result is
explicitly inconclusive. Retry once the competing dso central operation has
released the lock.

This command never performs recovery or cleanup: it does not delete, rename,
move, repair or install anything, and it appends no audit event.

```
dso central recovery-status [flags]
```

### Options

```
  -h, --help   help for recovery-status
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

