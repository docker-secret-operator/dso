import { useEffect, useState } from 'react'

export interface RestartPolicyInfo {
  name: string
  max_retry_count: number
  backoff_strategy: string
}

export interface NetworkInfo {
  ip: string
  gateway: string
  networks: string[]
}

export interface DSOAwarenessInfo {
  status: 'managed' | 'unmanaged' | 'partial'
  managed_secrets: string[]
  config_refs: string[]
  missing_mappings: string[]
}

export interface ContainerMetadata {
  id: string
  name: string
  image: string
  status: string
  created: string
  state: string
  restart_policy: RestartPolicyInfo
  network: NetworkInfo
  environment_variable_names: string[]
  sensitive_variable_count: number
  dso_awareness: DSOAwarenessInfo
  labels: Record<string, string>
}

export interface DiscoveryResponse {
  containers: ContainerMetadata[]
  total_count: number
  managed_count: number
  unmanaged_count: number
  partial_count: number
  timestamp: string
}

export interface SecretMappingSuggestion {
  container_id: string
  container_name: string
  env_var_name: string
  confidence: 'high' | 'medium' | 'low'
  reason: string
  suggested_secret_name: string
  configured_secret?: {
    secret_name: string
    provider: string
    is_mapped: boolean
  }
}

export interface MappingResponse {
  suggestions: SecretMappingSuggestion[]
  total_count: number
  timestamp: string
}

export interface CacheMetrics {
  cache_hits: number
  cache_misses: number
  refresh_count: number
  refresh_latency_ms: number
  cache_age_ms: number
  is_fresh: boolean
  timestamp: string
}

export function useDiscovery() {
  const [containers, setContainers] = useState<ContainerMetadata[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [discovery, setDiscovery] = useState<DiscoveryResponse | null>(null)

  const loadContainers = async () => {
    try {
      setLoading(true)
      setError(null)

      const response = await fetch('/api/discovery/docker')
      if (!response.ok) {
        throw new Error(`Failed to fetch containers: ${response.statusText}`)
      }
      const data = (await response.json()) as DiscoveryResponse
      setDiscovery(data)
      setContainers(data.containers)
    } catch (err) {
      setError(`Failed to load containers: ${err instanceof Error ? err.message : 'Unknown error'}`)
    } finally {
      setLoading(false)
    }
  }

  const refresh = async () => {
    await loadContainers()
  }

  useEffect(() => {
    loadContainers()
  }, [])

  return {
    containers,
    discovery,
    loading,
    error,
    refresh,
  }
}

export function useSecretMappings() {
  const [suggestions, setSuggestions] = useState<SecretMappingSuggestion[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [mappings, setMappings] = useState<MappingResponse | null>(null)

  const loadMappings = async () => {
    try {
      setLoading(true)
      setError(null)

      const response = await fetch('/api/discovery/docker/mappings')
      if (!response.ok) {
        throw new Error(`Failed to fetch mappings: ${response.statusText}`)
      }
      const data = (await response.json()) as MappingResponse
      setMappings(data)
      setSuggestions(data.suggestions)
    } catch (err) {
      setError(`Failed to load mappings: ${err instanceof Error ? err.message : 'Unknown error'}`)
    } finally {
      setLoading(false)
    }
  }

  const refresh = async () => {
    await loadMappings()
  }

  useEffect(() => {
    loadMappings()
  }, [])

  return {
    suggestions,
    mappings,
    loading,
    error,
    refresh,
  }
}

export function useDiscoveryMetrics() {
  const [metrics, setMetrics] = useState<CacheMetrics | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadMetrics = async () => {
    try {
      setLoading(true)
      setError(null)

      const response = await fetch('/api/discovery/metrics')
      if (!response.ok) {
        throw new Error(`Failed to fetch metrics: ${response.statusText}`)
      }
      const data = (await response.json()) as CacheMetrics
      setMetrics(data)
    } catch (err) {
      setError(`Failed to load metrics: ${err instanceof Error ? err.message : 'Unknown error'}`)
    } finally {
      setLoading(false)
    }
  }

  const refresh = async () => {
    await loadMetrics()
  }

  // Load metrics on mount and periodically
  useEffect(() => {
    loadMetrics()
    const interval = setInterval(loadMetrics, 5000) // Update every 5 seconds
    return () => clearInterval(interval)
  }, [])

  return {
    metrics,
    loading,
    error,
    refresh,
  }
}
