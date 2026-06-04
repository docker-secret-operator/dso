## dso config edit

Edit DSO configuration in your default editor

### Synopsis

Edit opens the DSO configuration file in your default text editor.

The editor is determined by:
1. $EDITOR environment variable
2. $VISUAL environment variable
3. 'nano' as fallback

Changes are automatically validated after saving.

Examples:
  docker dso config edit              # Edit default config
  docker dso config edit -c custom.yaml  # Edit specific config file

```
dso config edit [flags]
```

### Options

```
  -h, --help   help for edit
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso config](dso_config.md)	 - Manage DSO configuration

