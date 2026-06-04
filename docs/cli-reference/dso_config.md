## dso config

Manage DSO configuration

### Synopsis

Manage DSO configuration files.

Config provides commands to view, edit, and validate the DSO configuration.

Examples:
  docker dso config show              # View current configuration
  docker dso config edit              # Edit configuration in $EDITOR
  docker dso config validate          # Validate configuration for errors

```
dso config [flags]
```

### Options

```
  -h, --help   help for config
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose
* [dso config edit](dso_config_edit.md)	 - Edit DSO configuration in your default editor
* [dso config show](dso_config_show.md)	 - Display current DSO configuration
* [dso config validate](dso_config_validate.md)	 - Validate DSO configuration for errors

