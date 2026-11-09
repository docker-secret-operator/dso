## dso notifications

Manage rotation-event notification destinations

### Synopsis

Manage rotation-event notification destinations.

See docs/notifications.md for the event contract, delivery semantics, and
security model.

```
dso notifications [flags]
```

### Options

```
  -h, --help   help for notifications
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose
* [dso notifications test](dso_notifications_test.md)	 - Send a synthetic test event to every configured notification destination

