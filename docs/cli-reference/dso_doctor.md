## dso doctor

Diagnose the local DSO environment and current project

### Synopsis

Diagnose the local DSO environment and current project.

Doctor performs safe, read-only checks: Docker connectivity, DSO
configuration validity, configured provider credentials, and (when run
inside a Compose project) whether the project has a compose file, a
plaintext .env file, and well-formed DSO secret references.

Doctor never prints secret values, tokens, or credentials, and never
modifies any files.

Examples:
  docker dso doctor              # Quick health check
  docker dso doctor --level full # Include recovery steps for failures
  docker dso doctor --json       # Machine-readable output

```
dso doctor [flags]
```

### Options

```
  -h, --help           help for doctor
      --json           Output as JSON
      --level string   Diagnostic level: default, full (default "default")
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

