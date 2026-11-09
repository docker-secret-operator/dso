## dso secret get

Retrieve a secret from the vault

### Synopsis

Print a secret's value to stdout, without a trailing newline unless -n is given.

<project>/<path> selects the secret. If no project is given the default
project "global" is used (the same default as 'secret set', 'secret list' and
'env import'); a note is written to stderr when this happens.

```
dso secret get <project>/<path> [flags]
```

### Options

```
  -h, --help      help for get
  -n, --newline   Append a newline to the output
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso secret](dso_secret.md)	 - Manage DSO Native Vault secrets

