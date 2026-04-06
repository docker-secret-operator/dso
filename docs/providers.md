# Secret Provider Setup Guide (V3.1)

The Docker Secret Operator (DSO) integrates with multiple cloud and enterprise secret store backends. This guide outlines how to configure each provider.

---

## 🔐 AWS Secrets Manager

The standard provider for AWS environments. It supports native IAM authentication and secret rotation events.

### `dso.yaml` Configuration

```yaml
providers:
  aws:
    type: aws
    region: us-east-1
    auth:
      method: iam_role  # Automatically fetches from Instance Metadata Service (ECS/EC2)
```

---

## 🔐 HashiCorp Vault

The industry standard for multi-cloud and on-premise secret management.

### `dso.yaml` Configuration

```yaml
providers:
  vault:
    type: vault
    address: "https://vault.example.com:8200"
    token: "..."
    mount: "secret"  # Default KV v2 mount path
```

---

## 🔐 Azure Key Vault

Centralized secret storage for the Azure ecosystem.

### `dso.yaml` Configuration

```yaml
providers:
  azure:
    type: azure
    config:
      vault_url: "https://YOUR_VAULT_NAME.vault.azure.net/"
```

---

## 🔐 Huawei Cloud CSMS

Secret management for Huawei Cloud workloads.

### `dso.yaml` Configuration

```yaml
providers:
  huawei:
    type: huawei
    region: ap-southeast-2
    project_id: "..."
```

---

## 🔐 Local File Provider

Specialized provider for local development or air-gapped systems using the host's filesystem.

### `dso.yaml` Configuration

```yaml
providers:
  local:
    type: file
    config:
      path: "/var/lib/dso/local-secrets"
```

---

## 🛡️ Authentication Best Practices

1.  **Use IAM Roles**: Whenever possible (AWS/Huawei/Azure), use native IAM roles/agencies instead of long-lived access keys.
2.  **Least Privilege**: Ensure the DSO credentials only have `GetSecretValue` permissions for the specific secrets it needs to manage.
3.  **Environment Isolation**: Use separate providers for development, staging, and production in your `dso.yaml` map.
