## dso repair permissions

Inspect and repair the DSO directory permission contract

### Synopsis

Inspect the DSO permission contract and correct directory-level deviations.

Checked paths (and only these):
  /etc/dso                          root:dso 0750
  /var/lib/dso                      root:dso 0750
  /var/run/dso (/run/dso)           root:dso 0750
  /var/lib/dso/ledger               root 0700
  /var/lib/dso/ledger/ledger.key    root 0400 (0600 accepted)
  /usr/local/lib/dso/plugins        root-owned, no group/world write

(root:root replaces root:dso when the host has no 'dso' group.)

What is repaired: the owner and mode of the directories above, one directory at
a time, through a descriptor opened with O_NOFOLLOW|O_DIRECTORY. A symlink or a
regular file at one of the paths is refused. A directory that is already
correct is not touched, so a second run changes nothing.

What is never done: no recursive chmod or chown; no configuration file (dso.yaml)
is read, rewritten or replaced; the ledger key is never created, regenerated,
renamed or removed (a missing, unsafe or corrupt key is reported with a
diagnostic, and its mode is only tightened when the file is verifiably a ledger
key owned by root); authentication state is not reset; the agent is not
restarted; no unrelated file is modified. Restart the agent yourself afterwards
if it was running with the old permissions.

Applying changes requires root. --dry-run needs no root and changes nothing.

Exit status:
  0   every contract path conforms, or all required corrections were applied
  1   not root (apply), a correction failed, or another error
  12  --dry-run: corrections are required (nothing was changed)
  13  a path needs manual action (symlink, wrong owner, missing or corrupt key)

Examples:
  docker dso repair permissions --dry-run   # show current vs expected, change nothing
  sudo docker dso repair permissions        # apply the directory-level corrections

```
dso repair permissions [flags]
```

### Options

```
      --dry-run   Show current versus expected owner and mode; change nothing (no root needed)
  -h, --help      help for permissions
      --json      Output as JSON
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso repair](dso_repair.md)	 - Repair DSO host state that diagnostics reported as wrong

