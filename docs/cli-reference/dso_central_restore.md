## dso central restore

Validate, and optionally perform, a restore of the central database from a backup

### Synopsis

Validates a backup candidate (SQLite integrity, schema compatibility, full
audit-chain self-consistency, and root-key binding) and, unless --dry-run is
given, replaces the active database with it after creating and verifying a
mandatory safety snapshot of the active database and obtaining explicit
confirmation.

Known limitations:
  - audit-chain verification cannot detect that an older, internally valid
    backup is being restored over a database with more recent activity.
  - successful validation does not prove every secret version's ciphertext
    in the candidate is decryptable; it proves SQLite structural integrity
    and that the configured root key matches the candidate's binding.
  - this command has no caller-specific authorization today (the authn/authz
    design document it was planned against is not available in this
    checkout): anyone able to run 'dso central' as the owning OS user can
    run it.

Restore is an OFFLINE operator action. Stop every other process that uses the
active database before restoring. Restore takes no cross-process lock, so a
write made by another process while it runs can be silently lost.

--dry-run performs every validation check and reports the result without
touching the active database, creating a safety snapshot, or recording a
restore event.

```
dso central restore [flags]
```

### Options

```
      --dry-run       validate the candidate and report the result without modifying the active database
      --force         skip the interactive confirmation prompt (non-interactive restore)
      --from string   path to the backup to restore from (required)
  -h, --help          help for restore
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

