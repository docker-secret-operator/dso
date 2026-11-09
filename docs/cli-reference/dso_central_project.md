## dso central project

Manage projects

```
dso central project [flags]
```

### Options

```
  -h, --help   help for project
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
* [dso central project create](dso_central_project_create.md)	 - Create a project (generates its data key)
* [dso central project list](dso_central_project_list.md)	 - List projects

