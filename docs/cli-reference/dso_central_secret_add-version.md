## dso central secret add-version

Add a new immutable version (value from a hidden prompt or stdin)

```
dso central secret add-version <project> <environment> <name> [flags]
```

### Options

```
      --comment string   free-text note stored with the version (not secret)
  -h, --help             help for add-version
      --value-stdin      read the value from stdin instead of a hidden prompt
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

