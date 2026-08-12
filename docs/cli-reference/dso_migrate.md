## dso migrate

Migrate an existing .env + Compose project to DSO-managed secrets

### Synopsis

Migrate an existing .env + Compose project to DSO-managed secrets.

Scans the current directory for a Compose file and a .env file, proposes
which environment variables look like secrets, and -- only after an
explicit preview and confirmation -- imports the selected values into the
DSO vault and writes a new docker-compose.dso.yml with dso:// references
in their place.

Your original docker-compose.yml and .env are never modified or deleted.

Examples:
  docker dso migrate              # interactive: preview, then ask to proceed
  docker dso migrate --dry-run    # preview only; never touches the vault or filesystem
  docker dso migrate --confirm    # apply without an interactive prompt (CI)

```
dso migrate [flags]
```

### Options

```
      --confirm           Apply without an interactive prompt (for CI/non-interactive use)
      --dry-run           Preview the migration plan; never modifies the vault or filesystem
      --env-file string   Path to the .env file to migrate (default ".env")
      --file string       Path to the Compose file (default: auto-detected)
  -h, --help              help for migrate
      --overwrite         Overwrite existing vault secrets that differ from the .env value (default: skip them)
      --project string    Vault project name (default: current directory name)
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

