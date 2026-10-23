## dso

Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

### Synopsis

Docker Secret Operator (DSO) is a cloud-native secret injection runtime for Docker Compose.
It fetches and injects secrets into containers at runtime without exposing them to the host filesystem.

Quick start:
  docker dso setup                  # Interactive setup wizard (recommended)

Usage:
  docker dso setup                  # Interactive setup wizard (handles everything)

Advanced Usage (Manual Bootstrapping):
  docker dso bootstrap local        # Lower-level manual local mode init
  sudo docker dso bootstrap agent   # Lower-level manual production init

Quick reference:
  docker dso doctor                 # Validate environment
  docker dso status                 # Check operational status
  docker dso config show            # View configuration

DSO supports multiple secret backends: local vault, HashiCorp Vault, AWS Secrets Manager, and Azure Key Vault.

```
dso [flags]
```

### Options

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
  -h, --help            help for dso
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso agent](dso_agent.md)	 - Run the DSO background reconciliation engine
* [dso apply](dso_apply.md)	 - Apply declarative secret configuration
* [dso bootstrap](dso_bootstrap.md)	 - Initialize DSO runtime environment
* [dso central](dso_central.md)	 - Local administration of the central secrets store (experimental, local-only, no authentication)
* [dso compose](dso_compose.md)	 - Wrapper around docker compose that injects secrets
* [dso config](dso_config.md)	 - Manage DSO configuration
* [dso diff](dso_diff.md)	 - Not implemented: compare local configuration with a deployed stack
* [dso doctor](dso_doctor.md)	 - Diagnose the local DSO environment and current project
* [dso down](dso_down.md)	 - Stop and remove the stack's containers and networks (volumes and images are kept by default)
* [dso env](dso_env.md)	 - Manage .env file operations
* [dso export](dso_export.md)	 - Export injected secrets for local CI or testing
* [dso fetch](dso_fetch.md)	 - Manually fetch a secret and display it
* [dso init](dso_init.md)	 - Initialize the DSO Native Vault
* [dso inject](dso_inject.md)	 - Inject secrets directly into a running container
* [dso inspect](dso_inspect.md)	 - Inspect injected secrets for a running container
* [dso logs](dso_logs.md)	 - View DSO agent logs
* [dso migrate](dso_migrate.md)	 - Migrate an existing .env + Compose project to DSO-managed secrets
* [dso notifications](dso_notifications.md)	 - Manage rotation-event notification destinations
* [dso secret](dso_secret.md)	 - Manage DSO Native Vault secrets
* [dso setup](dso_setup.md)	 - Simple setup wizard for DSO
* [dso status](dso_status.md)	 - Show DSO runtime operational status
* [dso sync](dso_sync.md)	 - Trigger immediate secret synchronization
* [dso system](dso_system.md)	 - System-level DSO management
* [dso up](dso_up.md)	 - Deploy a stack and automatically start the DSO agent
* [dso validate](dso_validate.md)	 - Validate a DSO-managed Compose project (read-only, safe for CI)
* [dso version](dso_version.md)	 - Print the version number of DSO
* [dso watch](dso_watch.md)	 - Real-time monitor of secret rotations and container lifecycles

