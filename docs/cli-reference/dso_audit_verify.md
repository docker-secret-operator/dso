## dso audit verify

Verify the agent's durable audit file hash chain (read-only)

### Synopsis

Recomputes the hash chain of the agent's durable audit file
(default /var/lib/dso/state/audit.jsonl) and reports whether it is internally
consistent. It never modifies, creates or repairs the file. This is NOT the
central store's audit log; see `dso central audit verify` for that.

An unterminated final line (a write that was never acknowledged, for example
interrupted by a crash) is ignored, exactly as the agent ignores it on restart.

Exit status:
  0   verified (an empty file is verified)
  1   usage error, or any other command failure
  10  integrity failure: a record, link or sequence number does not match
  11  verification unavailable: the file is missing, unreadable, or not a
      regular file; this is not evidence of tampering
Codes 10 and 11 are shared with `dso central audit verify`.

--json prints exactly one document on stdout, also on failure.

Limitations. The chain is unkeyed: anyone who can write the file can recompute
it, so "verified" does NOT prove authenticity. It cannot detect deletion of the
newest records, replacement by an older valid copy, or events that happened
while the agent could not persist them (see dso_audit_durable_write_failures_total).
The audit file is best effort, not a guaranteed or fail-closed audit trail.

```
dso audit verify [flags]
```

### Options

```
      --file string   audit file to verify (default: the agent's durable audit file)
  -h, --help          help for verify
      --json          print the result as one JSON document
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso audit](dso_audit.md)	 - Inspect the agent's durable audit file

