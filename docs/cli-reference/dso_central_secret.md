## dso central secret

Manage versioned secrets

```
dso central secret [flags]
```

### Options

```
  -h, --help   help for secret
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
* [dso central secret add-version](dso_central_secret_add-version.md)	 - Add a new immutable version (value from a hidden prompt or stdin)
* [dso central secret create](dso_central_secret_create.md)	 - Create a secret (value from a hidden prompt or stdin)
* [dso central secret delete](dso_central_secret_delete.md)	 - Soft-delete a secret (versions retained)
* [dso central secret disable-version](dso_central_secret_disable-version.md)	 - Disable a version (kept, but no longer readable)
* [dso central secret get](dso_central_secret_get.md)	 - Print a secret value (requires --reveal)
* [dso central secret list](dso_central_secret_list.md)	 - List secret names and metadata (no values)
* [dso central secret versions](dso_central_secret_versions.md)	 - List version metadata (no values)

