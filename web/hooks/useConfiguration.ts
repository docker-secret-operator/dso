import { useEffect, useState } from 'react'

export interface ConfigStatus {
  status: string
  path: string
  last_modified: string
  valid: boolean
  validation_errors: string[]
  secret_count: number
  providers: Record<string, any>
  agent_configuration: any
}

export interface ProviderInfo {
  name: string
  type: string
  status: string
  error?: string
}

export interface ProvidersData {
  available: Record<string, any>
  active: Record<string, ProviderInfo>
}

export interface ProviderTestResult {
  success: boolean
  status: string
  error?: string
  latency_ms?: number
}

function getAuthHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('dso_api_token') : null
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export function useConfiguration() {
  const [config, setConfig] = useState<ConfigStatus | null>(null)
  const [providers, setProviders] = useState<ProvidersData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [testingProvider, setTestingProvider] = useState<string | null>(null)
  const [testResults, setTestResults] = useState<Record<string, ProviderTestResult>>({})

  // Load configuration
  const loadConfig = async () => {
    try {
      setLoading(true)
      setError(null)

      const configResponse = await fetch('/api/config', {
        headers: getAuthHeaders(),
      })
      const configData = (await configResponse.json()) as ConfigStatus
      setConfig(configData)

      if (!configData.valid) {
        setError('Configuration is invalid. See errors below.')
      }
    } catch (err) {
      setError(`Failed to load configuration: ${err instanceof Error ? err.message : 'Unknown error'}`)
    } finally {
      setLoading(false)
    }
  }

  // Load providers
  const loadProviders = async () => {
    try {
      const providersResponse = await fetch('/api/config/providers', {
        headers: getAuthHeaders(),
      })
      const providersData = (await providersResponse.json()) as ProvidersData
      setProviders(providersData)
    } catch (err) {
      console.error('Failed to load providers:', err)
    }
  }

  // Test provider connectivity
  const testProvider = async (providerName: string) => {
    try {
      setTestingProvider(providerName)
      const response = await fetch(`/api/config/providers/${providerName}/test`, {
        method: 'POST',
        headers: getAuthHeaders(),
      })
      const result = (await response.json()) as ProviderTestResult
      setTestResults((prev) => ({
        ...prev,
        [providerName]: result,
      }))
    } catch (err) {
      setTestResults((prev) => ({
        ...prev,
        [providerName]: {
          success: false,
          status: 'error',
          error: err instanceof Error ? err.message : 'Unknown error',
        },
      }))
    } finally {
      setTestingProvider(null)
    }
  }

  // Refresh configuration
  const refresh = async () => {
    await loadConfig()
    await loadProviders()
  }

  // Initial load
  useEffect(() => {
    loadConfig()
    loadProviders()
  }, [])

  return {
    config,
    providers,
    loading,
    error,
    testingProvider,
    testResults,
    refresh,
    testProvider,
  }
}
