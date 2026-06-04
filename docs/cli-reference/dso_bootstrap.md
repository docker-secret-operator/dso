## dso bootstrap

Initialize DSO runtime environment

### Synopsis

Initialize DSO for either local development or production agent mode.

Bootstrap creates the runtime directory structure, generates configuration,
initializes encryption, and validates your environment.

Examples:
  docker dso bootstrap local                           # For local development
  sudo docker dso bootstrap agent                      # For production deployment
  sudo docker dso bootstrap agent --enable-nonroot     # Production + non-root CLI access
  sudo docker dso bootstrap agent --provider aws --non-interactive  # Automated setup

```
dso bootstrap [local|agent] [flags]
```

### Options

```
      --aws-region string          AWS region for automated setup (default: us-east-1)
      --azure-vault-url string     Azure Key Vault URL for automated setup
      --enable-nonroot             Automatically configure current user for non-root DSO access (agent mode only)
  -h, --help                       help for bootstrap
      --huawei-project-id string   Huawei Project ID for automated setup
      --huawei-region string       Huawei region for automated setup
      --non-interactive            Non-interactive mode (skip all prompts, use defaults)
      --provider string            Cloud provider: aws, azure, vault, huawei (skips provider selection)
      --vault-address string       HashiCorp Vault address for automated setup
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

