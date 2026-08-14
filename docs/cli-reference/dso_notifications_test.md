## dso notifications test

Send a synthetic test event to every configured notification destination

### Synopsis

Send a synthetic test event to every configured notification destination.

Loads dso.yaml's notifications.webhooks, builds a real WebhookNotifier for
each, and delivers ONE synthetic event to each destination synchronously
(no queue, no retries beyond each destination's own max_retries) so
success/failure is reported immediately per destination.

The test event's secret_name is the literal string "test-secret" and
carries no real project data -- this command reads configuration only,
never the vault or any provider.

Exit code 0 means every configured destination accepted the test event;
non-zero means at least one did not.

```
dso notifications test [flags]
```

### Options

```
  -h, --help   help for test
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso notifications](dso_notifications.md)	 - Manage rotation-event notification destinations

