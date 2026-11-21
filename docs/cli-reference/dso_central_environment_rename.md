## dso central environment rename

Rename an environment's slug and/or display name

### Synopsis

Renames an environment. --name is optional: when it is omitted, the
environment's current display name is kept (the underlying store call always
sets both fields together, so the CLI looks the current name up first rather
than risk clearing it).

```
dso central environment rename <project> <environment> <new-slug> [flags]
```

### Options

```
  -h, --help          help for rename
      --json          print the result as one JSON document
      --name string   new display name (default: keep the current name)
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

* [dso central environment](dso_central_environment.md)	 - Manage environments

