## dso validate

Validate a DSO-managed Compose project (read-only, safe for CI)

### Synopsis

Validate a DSO-managed Compose project.

Checks Compose syntax and structure, the syntax of every dso:// / dsofile://
reference, whether referenced secrets exist in the local vault, and the
configured provider's credentials -- without modifying any file, importing
any secret, or printing any secret value.

validate never prompts, never mutates project or vault state, and is safe
to run in CI. Exit code 0 means valid; non-zero means at least one check
failed.

"Valid" describes configuration correctness as far as these checks can
determine -- it does not guarantee the project will deploy successfully
(e.g. Docker/network conditions at deploy time are out of scope; see
'docker dso doctor' for environment-level checks).

Examples:
  docker dso validate              # human-readable report
  docker dso validate --json       # machine-readable, for CI/scripts

```
dso validate [flags]
```

### Options

```
      --file string   Path to the Compose file (default: auto-detected)
  -h, --help          help for validate
      --json          Output as JSON
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

