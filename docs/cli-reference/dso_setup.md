## dso setup

Configure DSO for your environment

### Synopsis

Runs the DSO setup engine, which configures your environment through a structured pipeline:

1. **Detect** — discovers Docker, cloud provider metadata, and system capabilities
2. **Validate** — checks that the detected environment can support the requested mode
3. **Plan** — generates a declarative install plan (files, directories, services, groups)
4. **Preview** — displays what will be applied before any changes are made
5. **Apply** — executes the plan transactionally; every operation is recorded for rollback
6. **Rollback** — if any step fails, previously applied operations are reversed automatically

After setup, use `docker dso doctor` to validate the installation and `docker dso doctor --repair` to fix any issues found.

Examples:
  docker dso setup                              # Auto-detect mode and provider
  docker dso setup --mode local                 # Local vault mode (no cloud required)
  docker dso setup --mode agent --provider aws  # Cloud agent mode with AWS
  docker dso setup --auto-detect                # Auto-detect cloud provider from instance metadata
  docker dso setup --dry-run                    # Preview the plan without applying anything

```
dso setup [flags]
```

### Options

```
      --auto-detect       Auto-detect cloud provider from instance metadata
      --enable-nonroot    Enable non-root user access to DSO
  -h, --help              help for setup
      --mode string       Deployment mode: local or agent (cloud)
      --provider string   Cloud provider: aws, azure, vault, huawei
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

