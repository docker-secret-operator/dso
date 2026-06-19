import { ContainerMetadata, SecretMappingSuggestion, DiscoveryMetrics } from '@/lib/api/types'

export const mockContainers: ContainerMetadata[] = [
  {
    container_id: 'abc123def456',
    container_name: 'api-server-prod',
    image: 'registry.example.com/api-server:v1.2.3',
    status: 'running',
    networks: { ip: '172.17.0.2', gateway: '172.17.0.1', networks: ['bridge'] },
    env_vars: {
      DB_HOST: 'postgres.prod.internal',
      DB_PORT: '5432',
      REDIS_URL: 'redis://cache.prod.internal:6379',
      SECRET_KEY: 'should-be-in-vault',
    },
    dso_awareness: {
      status: 'partial',
      managed_secrets: ['DB_PASSWORD'],
      config_refs: ['DB_HOST', 'REDIS_URL'],
      missing_mappings: ['SECRET_KEY'],
    },
    labels: { env: 'production', team: 'platform' },
    restart_policy: { name: 'always', maximum_retry_count: 0 },
  } as any,
  {
    container_id: 'xyz789uvw012',
    container_name: 'database-primary',
    image: 'postgres:15-alpine',
    status: 'running',
    networks: { ip: '172.17.0.3', gateway: '172.17.0.1', networks: ['bridge'] },
    env_vars: { POSTGRES_PASSWORD: 'hardcoded-password', PGDATA: '/var/lib/postgresql/data' },
    dso_awareness: {
      status: 'unmanaged',
      managed_secrets: [],
      config_refs: [],
      missing_mappings: ['POSTGRES_PASSWORD', 'PGDATA'],
    },
    labels: { env: 'production', component: 'database' },
    restart_policy: { name: 'unless-stopped', maximum_retry_count: 0 },
  } as any,
  {
    container_id: 'pqr345stu678',
    container_name: 'cache-redis',
    image: 'redis:7-alpine',
    status: 'running',
    networks: { ip: '172.17.0.4', gateway: '172.17.0.1', networks: ['bridge'] },
    env_vars: { REDIS_PASSWORD: 'secure-password' },
    dso_awareness: {
      status: 'managed',
      managed_secrets: ['REDIS_PASSWORD', 'REDIS_AUTH_TOKEN'],
      config_refs: ['REDIS_HOST'],
      missing_mappings: [],
    },
    labels: { env: 'production', component: 'cache' },
    restart_policy: { name: 'always', maximum_retry_count: 0 },
  } as any,
  {
    container_id: 'jkl901mno234',
    container_name: 'worker-background',
    image: 'registry.example.com/worker:latest',
    status: 'stopped',
    networks: { ip: '172.17.0.5', gateway: '172.17.0.1', networks: ['bridge'] },
    env_vars: { QUEUE_URL: 'sqs://queue.aws.internal', API_KEY: 'demo-key' },
    dso_awareness: {
      status: 'partial',
      managed_secrets: ['API_KEY'],
      config_refs: ['QUEUE_URL'],
      missing_mappings: ['WORKER_TOKEN'],
    },
    labels: { env: 'staging', component: 'worker' },
    restart_policy: { name: 'no', maximum_retry_count: 0 },
  } as any,
]

export const mockMappings: SecretMappingSuggestion[] = [
  {
    env_var_name: 'DB_PASSWORD',
    suggested_secret_name: 'postgres-password',
    confidence: 'high',
    reason: 'Environment variable contains "password" keyword',
    is_configured: false,
  },
  {
    env_var_name: 'API_KEY',
    suggested_secret_name: 'api-key-production',
    confidence: 'high',
    reason: 'Matches naming pattern for API credentials',
    is_configured: true,
  },
  {
    env_var_name: 'SECRET_KEY',
    suggested_secret_name: 'django-secret-key',
    confidence: 'medium',
    reason: 'Likely Django application secret',
    is_configured: false,
  },
  {
    env_var_name: 'REDIS_PASSWORD',
    suggested_secret_name: 'redis-auth-password',
    confidence: 'high',
    reason: 'Redis authentication credential',
    is_configured: true,
  },
]

export const mockMetrics: DiscoveryMetrics = {
  cache_hits: 1247,
  cache_misses: 89,
  refresh_count: 23,
  avg_latency_ms: 145,
  cache_age_seconds: 42,
}
