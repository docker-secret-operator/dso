import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "AWS Secrets Manager Setup",
  description: "Configure Docker Secret Operator to use AWS Secrets Manager for production secret rotation."
};

export default function AWSProviderPage() {
  return (
    <div>
      <h1>AWS Secrets Manager Integration</h1>

      <p>
        Use AWS Secrets Manager for production secret rotation. DSO automatically detects secret changes and rotates containers with zero downtime.
      </p>

      <h2>Prerequisites</h2>

      <ul>
        <li><strong>AWS Account:</strong> Active AWS account with permissions</li>
        <li><strong>IAM Permissions:</strong> Permissions to read Secrets Manager secrets</li>
        <li><strong>Region:</strong> Secrets Manager available in your region</li>
        <li><strong>DSO Agent:</strong> v3.5.1 installed and ready</li>
      </ul>

      <h2>Architecture</h2>

      <pre><code className="language-text">
AWS Secrets Manager
        ↓
   (secrets stored)
        ↓
DSO Agent (polls/webhook)
        ↓
   (detects change)
        ↓
Create new container with updated secret
        ↓
   (atomic swap)
        ↓
Running containers with new secret
      </code></pre>

      <h2>Authentication Options</h2>

      <h3>Option 1: IAM Role (Recommended for EC2)</h3>

      <p>
        Best for EC2 instances. Credentials managed by AWS.
      </p>

      <h4>Step 1: Create IAM Role</h4>

      <pre><code className="language-bash">
# AWS CLI command
aws iam create-role --role-name dso-agent \
  --assume-role-policy-document '{
    "Version": "2012-10-17",
    "Statement": [
      {
        "Effect": "Allow",
        "Principal": {
          "Service": "ec2.amazonaws.com"
        },
        "Action": "sts:AssumeRole"
      }
    ]
  }'
      </code></pre>

      <h4>Step 2: Add Inline Policy</h4>

      <pre><code className="language-bash">
aws iam put-role-policy --role-name dso-agent \
  --policy-name dso-secrets-policy \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [
      {
        "Effect": "Allow",
        "Action": [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret",
          "secretsmanager:ListSecrets"
        ],
        "Resource": "arn:aws:secretsmanager:*:ACCOUNT_ID:secret:prod/*"
      }
    ]
  }'
      </code></pre>

      <p>
        Replace <code>ACCOUNT_ID</code> with your AWS account ID. The <code>prod/*</code> restricts access to secrets starting with <code>prod/</code>.
      </p>

      <h4>Step 3: Attach to EC2 Instance</h4>

      <pre><code className="language-bash">
# Create instance profile
aws iam create-instance-profile --instance-profile-name dso-profile

# Add role to profile
aws iam add-role-to-instance-profile \
  --instance-profile-name dso-profile \
  --role-name dso-agent

# Attach to running instance
aws ec2 associate-iam-instance-profile \
  --iam-instance-profile Name=dso-profile \
  --instance-id i-0123456789abcdef0
      </code></pre>

      <h4>Step 4: Bootstrap Agent with IAM Role</h4>

      <pre><code className="language-bash">
sudo docker dso bootstrap agent

# When prompted:
# Provider type: aws
# Authentication method: iam-role
# AWS Region: us-east-1 (or your region)
# Secret prefix: prod/
# Event mode: polling or webhook
      </code></pre>

      <h3>Option 2: IAM User Credentials</h3>

      <p>
        For non-EC2 or testing. Create dedicated IAM user.
      </p>

      <h4>Step 1: Create IAM User</h4>

      <pre><code className="language-bash">
aws iam create-user --user-name dso-agent
      </code></pre>

      <h4>Step 2: Create Access Keys</h4>

      <pre><code className="language-bash">
aws iam create-access-key --user-name dso-agent
# Output:
# AccessKeyId: AKIAIOSFODNN7EXAMPLE
# SecretAccessKey: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
      </code></pre>

      <h4>Step 3: Attach Inline Policy</h4>

      <pre><code className="language-bash">
aws iam put-user-policy --user-name dso-agent \
  --policy-name dso-secrets-policy \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [
      {
        "Effect": "Allow",
        "Action": [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret",
          "secretsmanager:ListSecrets"
        ],
        "Resource": "arn:aws:secretsmanager:*:ACCOUNT_ID:secret:prod/*"
      }
    ]
  }'
      </code></pre>

      <h4>Step 4: Bootstrap Agent with User Credentials</h4>

      <pre><code className="language-bash">
sudo docker dso bootstrap agent

# When prompted:
# Provider type: aws
# Authentication method: access-key
# AWS Access Key ID: AKIAIOSFODNN7EXAMPLE
# AWS Secret Access Key: wJalrXUtnFEMI/...
# AWS Region: us-east-1
# Secret prefix: prod/
# Event mode: webhook
      </code></pre>

      <h3>Option 3: AWS CLI Profile</h3>

      <p>
        Use existing AWS CLI configuration.
      </p>

      <pre><code className="language-bash">
# Configure AWS CLI
aws configure --profile dso-agent
# AWS Access Key ID: AKIAIOSFODNN7EXAMPLE
# AWS Secret Access Key: wJalrXUtnFEMI/...
# Default region: us-east-1
# Default output format: json

# Bootstrap with profile
sudo docker dso bootstrap agent --aws-profile dso-agent
      </code></pre>

      <h2>Setup Secrets in AWS</h2>

      <h3>Create Secret</h3>

      <pre><code className="language-bash">
aws secretsmanager create-secret \
  --name prod/database-password \
  --secret-string "postgres123" \
  --region us-east-1
      </code></pre>

      <h3>Secret Naming Convention</h3>

      <p>
        Recommended naming pattern:
      </p>

      <pre><code className="language-text">
prod/service/secret-type

Examples:
prod/api/database-password
prod/api/api-key
prod/worker/kafka-password
prod/cache/redis-password
      </code></pre>

      <h3>Create Multiple Secrets</h3>

      <pre><code className="language-bash">
# Database credentials
aws secretsmanager create-secret \
  --name prod/database/password \
  --secret-string "postgres123" \
  --region us-east-1

# API keys
aws secretsmanager create-secret \
  --name prod/api/key \
  --secret-string "sk_live_abc123def456" \
  --region us-east-1

# OAuth tokens
aws secretsmanager create-secret \
  --name prod/auth/github-token \
  --secret-string "ghp_xxxxxxxxxxxxxxxxxxxx" \
  --region us-east-1
      </code></pre>

      <h2>Configure docker-compose.yml</h2>

      <h3>Basic Example</h3>

      <pre><code className="language-yaml">
version: '3.8'

services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD: ${prod/database/password}
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 3

  api:
    image: myapp:latest
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://postgres:${prod/database/password}@postgres:5432/app
      API_KEY: ${prod/api/key}
      GITHUB_TOKEN: ${prod/auth/github-token}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      </code></pre>

      <h3>Secret Path Mapping</h3>

      <p>
        DSO automatically replaces <code>${secret-name}</code> with values from AWS:
      </p>

      <pre><code className="language-yaml">
# In docker-compose.yml
DATABASE_PASSWORD: ${prod/database/password}

# DSO substitutes:
DATABASE_PASSWORD: postgres123  # From AWS Secrets Manager
      </code></pre>

      <h2>Configuration</h2>

      <h3>Agent Config File</h3>

      <p>
        Located at <code>/etc/dso/config.yml</code> after bootstrap.
      </p>

      <pre><code className="language-yaml">
agent:
  name: production-host-1
  mode: agent
  log_level: info

provider:
  type: aws-secrets-manager
  region: us-east-1
  secret_prefix: prod/
  auth_method: iam-role  # or access-key

aws:
  access_key_id: AKIAIOSFODNN7EXAMPLE  # Optional if using IAM role
  secret_access_key: wJalrXUtnFEMI/...  # Optional if using IAM role
  region: us-east-1
  endpoint: https://secretsmanager.us-east-1.amazonaws.com

discovery:
  mode: webhook  # or polling
  webhook_path: /webhook
  webhook_port: 8443
  polling_interval_seconds: 60  # If polling mode

observability:
  structured_logging: true
  metrics_port: 9090
  health_check_port: 8081
      </code></pre>

      <h2>Event Detection: Polling vs. Webhook</h2>

      <h3>Polling Mode</h3>

      <p>
        DSO periodically checks AWS for secret changes.
      </p>

      <pre><code className="language-yaml">
discovery:
  mode: polling
  polling_interval_seconds: 60
      </code></pre>

      <ul>
        <li><strong>Rotation Latency:</strong> Up to 60 seconds after secret change</li>
        <li><strong>Setup:</strong> No webhook configuration needed</li>
        <li><strong>API Calls:</strong> 1 per polling interval (minimal cost)</li>
        <li><strong>Firewall:</strong> No inbound ports needed</li>
      </ul>

      <h3>Webhook Mode (EventBridge)</h3>

      <p>
        AWS EventBridge triggers immediate rotation when secret changes.
      </p>

      <h4>Setup EventBridge Rule</h4>

      <pre><code className="language-bash">
# Create rule
aws events put-rule \
  --name dso-secret-changes \
  --event-pattern '{
    "source": ["aws.secretsmanager"],
    "detail-type": ["AWS API Call via CloudTrail"],
    "detail": {
      "eventName": ["PutSecretValue", "CreateSecret"],
      "requestParameters": {
        "secretId": [{
          "prefix": "prod/"
        }]
      }
    }
  }' \
  --state ENABLED

# Add target (DSO webhook)
aws events put-targets \
  --rule dso-secret-changes \
  --targets "Id"="1","Arn"="arn:aws:events:us-east-1:ACCOUNT_ID:target/dso-webhook","HttpParameters":{"HeaderParameters":{"X-Secret-Signature":"HMAC_KEY"}},"RoleArn"="arn:aws:iam::ACCOUNT_ID:role/event-bridge-role"
      </code></pre>

      <h4>Configure DSO Webhook</h4>

      <pre><code className="language-yaml">
discovery:
  mode: webhook
  webhook_path: /webhook
  webhook_port: 8443
  webhook_verify_signature: true
  webhook_signature_header: X-Secret-Signature
      </code></pre>

      <ul>
        <li><strong>Rotation Latency:</strong> 1-5 seconds (immediate)</li>
        <li><strong>Setup:</strong> Requires EventBridge rule</li>
        <li><strong>API Calls:</strong> Only on actual secret changes</li>
        <li><strong>Firewall:</strong> Need inbound HTTPS port</li>
      </ul>

      <h2>Deployment</h2>

      <h3>Start Agent</h3>

      <pre><code className="language-bash">
# Start systemd service
sudo systemctl start dso-agent

# Verify status
sudo systemctl status dso-agent

# View logs
sudo journalctl -u dso-agent -f
      </code></pre>

      <h3>Deploy Containers</h3>

      <pre><code className="language-bash">
# Create docker-compose.yml (as shown above)

# Deploy with secrets
docker compose up -d

# Verify secrets injected
docker ps
docker compose logs

# Check no secrets in docker inspect
docker inspect api-container | grep DATABASE_PASSWORD
# Should return nothing
      </code></pre>

      <h3>Update Secrets</h3>

      <pre><code className="language-bash">
# Update secret in AWS
aws secretsmanager update-secret \
  --secret-id prod/database/password \
  --secret-string "newpassword456" \
  --region us-east-1

# DSO automatically detects and rotates
# Check rotation status
sudo dso status

# View rotation history
sudo dso rotation history
      </code></pre>

      <h2>Monitoring & Troubleshooting</h2>

      <h3>Check Agent Status</h3>

      <pre><code className="language-bash">
# Service status
sudo systemctl status dso-agent

# Recent logs
sudo journalctl -u dso-agent -n 50

# Search for errors
sudo journalctl -u dso-agent | grep ERROR
      </code></pre>

      <h3>Verify AWS Credentials</h3>

      <pre><code className="language-bash">
# Test IAM role (if using)
aws sts get-caller-identity

# Test Secrets Manager access
aws secretsmanager list-secrets --filter Key=name,Values=prod/

# Test get secret
aws secretsmanager get-secret-value --secret-id prod/database/password
      </code></pre>

      <h3>Common Issues</h3>

      <h4>Issue: UnauthorizedException from AWS</h4>

      <pre><code className="language-text">
Error: User: arn:aws:iam::123456789:user/dso-agent is not authorized

Solution:
1. Check IAM policy attached to dso-agent user/role
2. Verify policy includes secretsmanager:GetSecretValue
3. Check secret name matches policy prefix (prod/*)
4. Check region is correct
      </code></pre>

      <h4>Issue: Secret Not Found</h4>

      <pre><code className="language-text">
Error: ResourceNotFoundException: Secrets Manager can't find the specified secret

Solution:
1. Verify secret exists in AWS: aws secretsmanager list-secrets
2. Check secret name matches docker-compose.yml
3. Check region matches agent config
4. Check secret prefix configuration
      </code></pre>

      <h4>Issue: Rotation Not Triggering</h4>

      <pre><code className="language-text">
Agent running but containers not rotating after secret update.

Solution (if polling):
1. Wait for next polling interval (up to 60s)
2. Check polling_interval_seconds in config

Solution (if webhook):
1. Verify EventBridge rule is enabled
2. Check DSO host is reachable from AWS
3. Check firewall allows inbound HTTPS
4. Test webhook: curl -X POST https://localhost:8443/webhook
      </code></pre>

      <h2>Cost Optimization</h2>

      <ul>
        <li><strong>Polling:</strong> ~1440 API calls/day (30 per hour, $0.40/day)</li>
        <li><strong>Webhook:</strong> Only API calls when secrets change (minimal cost)</li>
        <li><strong>Recommendation:</strong> Use webhook for production</li>
        <li><strong>Budget Alert:</strong> Set CloudWatch alarm for secrets API costs</li>
      </ul>

      <h2>Security Best Practices</h2>

      <ul>
        <li><strong>IAM Role:</strong> Use IAM role on EC2 (no credentials on disk)</li>
        <li><strong>Least Privilege:</strong> Restrict to specific secret names (prod/*)</li>
        <li><strong>Rotation:</strong> Consider AWS Secrets Manager automatic rotation</li>
        <li><strong>Encryption:</strong> Use AWS KMS for key management</li>
        <li><strong>Audit:</strong> Enable CloudTrail for secret access logging</li>
      </ul>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/getting-started">Getting started guide</a></li>
        <li><a href="/docs/guide/providers/azure">Azure Key Vault setup</a></li>
        <li><a href="/docs/guide/providers/vault">HashiCorp Vault setup</a></li>
        <li><a href="/docs/guide/observability">Monitoring and observability</a></li>
        <li><a href="/docs/guide/production-readiness">Production deployment checklist</a></li>
      </ul>
    </div>
  );
}
