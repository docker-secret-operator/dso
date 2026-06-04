## dso system setup

Setup DSO provider plugins

### Synopsis

Setup DSO system components like provider plugins.

When running from released binaries, provider plugins may not be installed.
Use this command to manually build and install them from source.

Examples:
  # Install AWS provider plugin
  docker dso system setup --provider aws

  # Install multiple providers
  docker dso system setup --provider aws --provider azure

```
dso system setup [flags]
```

### Options

```
  -h, --help               help for setup
      --provider strings   Provider(s) to install: aws, azure, vault, huawei (can specify multiple times)
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso system](dso_system.md)	 - System-level DSO management

