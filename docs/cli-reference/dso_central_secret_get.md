## dso central secret get

Print a secret value (requires --reveal)

```
dso central secret get <project> <environment> <name> [flags]
```

### Options

```
  -h, --help          help for get
      --reveal        print the plaintext value
      --version int   version number (default: the newest version; fails if it is disabled)
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

* [dso central secret](dso_central_secret.md)	 - Manage versioned secrets

