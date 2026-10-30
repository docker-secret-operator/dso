## dso central audit verify

Verify the integrity of the audit hash chain (read-only)

### Synopsis

Recomputes the audit log's SHA-256 hash chain and reports whether it is
internally consistent. It is strictly read-only: it needs no key, appends no
audit event, runs no migration and creates nothing. The database file is never
opened by SQLite; a private temporary copy of it (and of any -wal/-shm files)
is verified instead, so the original and its sidecars are not touched. The copy
needs temporary disk space about the size of the database and is removed when
the command finishes or is interrupted by SIGINT/SIGTERM. After SIGKILL or a
machine failure it cannot be removed automatically: a directory named
dso-restore-validate-* (mode 0700) can remain in the temporary directory and
should be deleted manually. A -wal or -shm file that is a symbolic link or not
a regular file makes verification unavailable.

An empty audit log (zero events) is reported as verified.

Exit status:
  0   verified
  1   usage error, or any other command failure
  10  integrity failure: a hash or link in the chain does not match
  11  verification unavailable: the database could not be verified (for
      example unreadable, unsafe, no audit log, unsupported schema); this is
      not evidence of tampering
Codes 10 and 11 apply only to this command. Scripts should treat any non-zero
exit status as failure.

--json prints exactly one document on stdout, also on failure:
  status            "verified", "integrity_failure" or "unavailable"
  event_count       number of events read (omitted when unavailable)
  tip_event_id      id of the last event (verified, non-empty chain only)
  failure_index     0-based position of the first failing event
  failure_event_id  id of the first failing event
  failure_reason    why it failed
  error             sanitized reason verification was unavailable (no paths;
                    details are written to stderr only)

Same-user access. The database is safety-checked and then copied by path, and
its -wal/-shm files are checked only for being regular files (not symbolic
links), not for owner or mode. A process running as the same operating-system
user with write access to the database directory can still race these checks;
they do not defend against that.

Concurrent changes. The verified copy is made with plain file reads, not a
transactionally consistent snapshot. If the database or its -wal file changes
while it is being copied, the copy is retried; if no stable copy can be
obtained the result is exit 11 (verification unavailable), which means the
check could not be done and is not evidence of tampering. Verify while no
process is writing to the database for the most reliable result.

Limitations. The chain is unkeyed: anyone who can modify the database can
recompute it, so a "verified" result does NOT prove authenticity. It also
cannot detect deletion of the newest events or replacement of the database by
an older, internally valid copy. This command is not authentication or
authorization, and not a tamper-proof guarantee.

```
dso central audit verify [flags]
```

### Options

```
  -h, --help   help for verify
      --json   print the result as one JSON document
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

* [dso central audit](dso_central_audit.md)	 - Inspect the audit log

