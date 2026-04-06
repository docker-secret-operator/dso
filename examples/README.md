# DSO Examples

This directory contains reference configurations for the Docker Secret Operator (DSO) V2.

## 📁 Repository Structure

| File | Description |
| :--- | :--- |
| `dso-v2.yaml` | **Full Production Reference** using multiple providers. |
| `dso-minimal.yaml` | Smallest working configuration (Starter). |
| `dso-aws.yaml` | AWS Secrets Manager specialized configuration. |
| `dso-local.yaml` | Local development reference using the `file` provider. |

## 🚀 How to Use

1.  **Choose an example**: Start with `dso-minimal.yaml` if you're new.
2.  **Edit the configuration**: Replace placeholders (like `YOUR_VAULT_TOKEN`) with actual values.
3.  **Run DSO**:
    ```bash
    docker dso up -d -c examples/dso-minimal.yaml
    ```

## 🔐 Specialized Integrations

We also provide complete subdirectory examples for specific cloud providers that include `docker-compose.yaml` and deployment notes:

- [AWS Secrets Manager](./aws-compose)
- [Azure Key Vault](./azure-compose)
- [HashiCorp Vault](./hashicorp-vault)
- [Huawei Cloud CSMS](./huawei-compose)

---
For a full configuration reference, see the [Configuration Guide](../docs/configuration.md).
