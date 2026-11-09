## dso config show

Display current DSO configuration

### Synopsis

Show displays the current DSO configuration file.

The configuration file location is determined by:
1. CLI flag (-c/--config)
2. /etc/dso/dso.yaml (system-wide)
3. ./dso.yaml (current directory)
4. ~/.dso/dso.yaml (per-user; used only if none of the above exist)

Run 'docker dso config path' to see which file is selected and why.

Examples:
  docker dso config show              # Show default config
  docker dso config show -c custom.yaml  # Show specific config file

```
dso config show [flags]
```

### Options

```
  -h, --help   help for show
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso config](dso_config.md)	 - Manage DSO configuration

