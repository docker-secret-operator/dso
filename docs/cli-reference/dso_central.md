## dso central

Local administration of the central secrets store (experimental, local-only, no authentication)

### Synopsis

Experimental M1 interface to the central secrets store.

This command operates directly on a local SQLite database and root key. It
exposes no network API and performs no authentication or authorization: access
control is the file permissions of the database and key file. Do not use it as
a production secrets workflow yet.

```
dso central [flags]
```

### Options

```
      --db string                path to the SQLite database (required)
  -h, --help                     help for central
      --key-file string          path to the root key file (required)
      --key-provider string      root key provider: file or passphrase (required)
      --passphrase-file string   read the passphrase from this file (mode 0600) instead of prompting
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose
* [dso central audit](dso_central_audit.md)	 - Inspect the audit log
* [dso central backup](dso_central_backup.md)	 - Write a verified, on-demand snapshot of the central database
* [dso central environment](dso_central_environment.md)	 - Manage environments
* [dso central init](dso_central_init.md)	 - Create the root key and an empty, key-bound database
* [dso central project](dso_central_project.md)	 - Manage projects
* [dso central restore](dso_central_restore.md)	 - Validate, and optionally perform, a restore of the central database from a backup
* [dso central secret](dso_central_secret.md)	 - Manage versioned secrets

