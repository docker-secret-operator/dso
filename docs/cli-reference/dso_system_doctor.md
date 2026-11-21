## dso system doctor

Diagnose the local DSO environment and current project

### Synopsis

Diagnose the local DSO environment and current project.

Doctor performs safe, read-only checks: Docker connectivity, DSO
configuration validity, configured provider credentials, and (when run
inside a Compose project) whether the project has a compose file, a
plaintext .env file, and well-formed DSO secret references.

Doctor never prints secret values, tokens, or credentials, and never
modifies any files.

The central secrets store (`dso central`) has no persisted configuration
of its own, so its root-key/database custody checks are opt-in: pass
--central-db and --central-key-file (the same paths given to `dso central`)
to include them.

Examples:
  docker dso doctor              # Quick health check
  docker dso doctor --level full # Include recovery steps for failures
  docker dso doctor --json       # Machine-readable output
  docker dso doctor --central-db ./central.db --central-key-file ./root.key --central-key-provider file

```
dso system doctor [flags]
```

### Options

```
      --central-db dso central        path to a central store SQLite database to check custody of (opt-in; same path given to dso central)
      --central-key-file string       path to the central store's root key (or KDF-parameter) file to check custody of (opt-in)
      --central-key-provider string   central store root key provider: file or passphrase (affects how --central-key-file is described, not how it is checked)
  -h, --help                          help for doctor
      --json                          Output as JSON
      --level string                  Diagnostic level: default, full (default "default")
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso system](dso_system.md)	 - System-level DSO management

