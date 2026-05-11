# DSO Examples (V3.2)

Complete, production-ready examples for Docker Secret Operator across different cloud providers and use cases.

## 🚀 Quick Start

### Local Development (File Backend)
Use the `file` provider for local development without cloud credentials:

```bash
mkdir -p .secrets
echo '{"password":"dev-pass"}' > .secrets/db-password.json

docker dso up -d -c examples/dso-local.yaml
```

### Cloud Provider Setup

Choose your cloud provider and follow the complete guide:

| Provider | Guide | Includes |
|----------|-------|----------|
| **AWS Secrets Manager** | [aws-compose/README.md](./aws-compose) | docker-compose.yaml, dso.yaml, setup steps |
| **Azure Key Vault** | [azure-compose/README.md](./azure-compose) | docker-compose.yaml, dso.yaml, setup steps |
| **HashiCorp Vault** | [hashicorp-vault/README.md](./hashicorp-vault) | docker-compose.yaml, dso.yaml, setup steps |
| **Huawei Cloud CSMS** | [huawei-compose/README.md](./huawei-compose) | docker-compose.yaml, dso.yaml, setup steps |

## 📖 Learning Path

1. **New to DSO?** Start with [Local Development](#local-development-file-backend) above
2. **Using Cloud?** Go to your cloud provider's directory above
3. **Need reference configs?** See [Configuration Guide](../docs/configuration.md)
4. **Full concepts?** Read [Concepts & Architecture](../docs/concepts.md)

## 📝 Configuration Files

Example configurations for different scenarios:

- `dso-local.yaml` — Local file-based backend (development)
- `dso-minimal.yaml` — Minimal working config template
- `dso.aws.yaml` — AWS production example (file injection, IAM roles)
- Cloud provider subdirectories — Complete working examples with docker-compose.yaml

---
For detailed configuration schema, see the [Configuration Reference](../docs/configuration.md).
