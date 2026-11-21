## dso init

Initialize the DSO Native Vault

### Synopsis

Creates the encrypted vault at ~/.dso/vault.enc. Must be run as a standard user (not root).

```
dso init [flags]
```

### Options

```
  -h, --help   help for init
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

