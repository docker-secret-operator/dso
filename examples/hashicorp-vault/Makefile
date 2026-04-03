.PHONY: up mysql down setup-vault help

# Default target
help:
	@echo "Usage:"
	@echo "  make up           - Start Vault in Dev Mode"
	@echo "  make setup-vault  - Configure secrets inside the Vault container"
	@echo "  make mysql        - Start MySQL container manually (for testing)"
	@echo "  make down         - Stop and remove all demo containers"

up:
	@echo "Starting Vault in Dev Mode..."
	docker run -d \
		--name vault \
		-p 8200:8200 \
		-e VAULT_DEV_ROOT_TOKEN_ID=root \
		hashicorp/vault
	@echo "Vault is running at http://localhost:8200"

setup-vault:
	@echo "Configuring Vault KV engine and secrets..."
	# Enable KV-V2 engine at 'secret' path (if not already enabled)
	docker exec vault sh -c "vault secrets enable -path=secret kv-v2" || true
	# Store MySQL credentials
	docker exec vault sh -c "vault kv put secret/mysql \
		username='root' \
		password='mypassword' \
		host='localhost' \
		port='3306'"
	@echo "Secrets configured successfully in Vault."

mysql:
	@echo "Starting standalone MySQL container..."
	docker run -d \
		--name mysql-demo \
		-e MYSQL_ROOT_PASSWORD=mypassword \
		-p 3306:3306 \
		mysql:8
	@echo "MySQL standalone running."

down:
	@echo "Cleaning up containers..."
	docker stop vault mysql-demo mysql-vault-demo 2>/dev/null || true
	docker rm vault mysql-demo mysql-vault-demo 2>/dev/null || true
	@echo "Cleanup complete."
