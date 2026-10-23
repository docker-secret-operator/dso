## dso central init

Create the root key and an empty, key-bound database

### Synopsis

Create the root key, an empty database, and bind the database to that key.

Everything that can be checked is checked BEFORE any key material is written
(directory safety, an existing database, an existing key), so the usual
failures leave nothing behind. If the key is installed but the database cannot
be created, the key is kept (it is never deleted or overwritten) and the
database files this run created are removed; fix the cause and re-run with
--use-existing-key to build the database around the key that is already there.

```
dso central init [flags]
```

### Options

```
      --allow-colocated-key   allow the key file in the same directory as the database (testing only)
  -h, --help                  help for init
      --use-existing-key      build the database around a key file that already exists (recovery after a failed init); the key is never modified
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

