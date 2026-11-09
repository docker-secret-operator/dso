## dso secret set

Set a secret securely in the vault

### Synopsis

Store a secret read from a hidden prompt or from stdin.

The value is stored byte-for-byte: leading/trailing whitespace, tabs, carriage
returns and trailing newlines are preserved. Piped input is NOT trimmed, so
'echo secret | dso secret set p/k' stores "secret\n". Use 'printf %s secret |',
'dso secret set p/k < file', or --trim-newline (removes one trailing newline).
Values must be valid UTF-8 and at most 1MB.

```
dso secret set <project>/<path> [flags]
```

### Options

```
  -h, --help           help for set
      --trim-newline   Remove one trailing newline from piped input (nothing else is trimmed)
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso secret](dso_secret.md)	 - Manage DSO Native Vault secrets

