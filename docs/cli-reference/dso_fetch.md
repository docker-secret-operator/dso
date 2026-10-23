## dso fetch

Manually fetch a secret and display it

### Synopsis

Fetch a secret from the configured provider and display its keys.

By default secret values are masked (shown as ***) to prevent accidental
exposure in terminal recordings and shared screens. Use --reveal to print
the actual values — only do this in a private terminal session.

```
dso fetch [secret-name] [flags]
```

### Options

```
  -h, --help     help for fetch
      --reveal   Print secret values in plaintext (use only in a private terminal)
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

