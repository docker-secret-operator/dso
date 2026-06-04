'use client'

import React, { useState } from 'react'
import { useConfiguration } from '@/hooks/useConfiguration'
import { ErrorBoundary } from '@/components/error-boundary'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { AlertCircle, CheckCircle2, RefreshCw, Server } from 'lucide-react'

export default function ConfigurationPage() {
  const { config, providers, loading, error, testingProvider, testResults, refresh, testProvider } =
    useConfiguration()
  const [expandedSection, setExpandedSection] = useState<string | null>(null)

  if (loading) {
    return (
      <div className="p-6">
        <div className="flex items-center justify-center h-64">
          <div className="text-center">
            <RefreshCw className="w-8 h-8 animate-spin mx-auto mb-2" />
            <p>Loading configuration...</p>
          </div>
        </div>
      </div>
    )
  }

  return (
    <ErrorBoundary>
      <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Configuration</h1>
          <p className="text-gray-600 mt-1">View and manage DSO configuration</p>
        </div>
        <Button onClick={refresh} variant="outline" size="sm">
          <RefreshCw className="w-4 h-4 mr-2" />
          Refresh
        </Button>
      </div>

      {/* Error Alert */}
      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 flex items-start gap-3">
          <AlertCircle className="w-5 h-5 text-red-600 flex-shrink-0 mt-0.5" />
          <div className="text-sm text-red-800">{error}</div>
        </div>
      )}

      {/* Configuration Overview Card */}
      {config && (
        <Card>
          <CardHeader>
            <CardTitle>Configuration Status</CardTitle>
            <CardDescription>Current configuration file and validation status</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Config Path */}
            <div className="grid grid-cols-2 gap-4">
              <div>
                <p className="text-sm text-gray-600 font-medium">Configuration File</p>
                <p className="text-sm font-mono bg-gray-50 p-2 rounded mt-1 break-all">{config.path}</p>
              </div>
              <div>
                <p className="text-sm text-gray-600 font-medium">Last Modified</p>
                <p className="text-sm mt-1">
                  {new Date(config.last_modified).toLocaleString()}
                </p>
              </div>
            </div>

            {/* Validation Status */}
            <div className="pt-2 border-t">
              <div className="flex items-center justify-between">
                <p className="text-sm text-gray-600 font-medium">Validation Status</p>
                {config.valid ? (
                  <Badge className="bg-green-100 text-green-800 flex items-center gap-1">
                    <CheckCircle2 className="w-3 h-3" />
                    Valid
                  </Badge>
                ) : (
                  <Badge variant="destructive" className="flex items-center gap-1">
                    <AlertCircle className="w-3 h-3" />
                    Invalid
                  </Badge>
                )}
              </div>
              {config.validation_errors && config.validation_errors.length > 0 && (
                <div className="mt-2 space-y-1">
                  {config.validation_errors.map((err, i) => (
                    <p key={i} className="text-xs text-red-600">
                      • {err}
                    </p>
                  ))}
                </div>
              )}
            </div>

            {/* Quick Stats */}
            <div className="pt-2 border-t grid grid-cols-3 gap-4">
              <div>
                <p className="text-xs text-gray-600 uppercase font-semibold">Secrets Configured</p>
                <p className="text-2xl font-bold text-gray-900 mt-1">{config.secret_count}</p>
              </div>
              <div>
                <p className="text-xs text-gray-600 uppercase font-semibold">Providers Active</p>
                <p className="text-2xl font-bold text-gray-900 mt-1">
                  {config.providers ? Object.keys(config.providers).length : 0}
                </p>
              </div>
              <div>
                <p className="text-xs text-gray-600 uppercase font-semibold">Status</p>
                <p className="text-2xl font-bold text-gray-900 mt-1 capitalize">{config.status}</p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Agent Configuration */}
      {config?.agent_configuration && (
        <Card>
          <CardHeader>
            <CardTitle>Agent Settings</CardTitle>
            <CardDescription>DSO agent configuration</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <p className="text-sm text-gray-600">Cache Enabled</p>
                  <Badge className="mt-1 bg-blue-100 text-blue-800">
                    {config.agent_configuration.cache_enabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                </div>
                <div>
                  <p className="text-sm text-gray-600">Auto-Sync</p>
                  <Badge className="mt-1 bg-blue-100 text-blue-800">
                    {config.agent_configuration.auto_sync_enabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                </div>
                <div>
                  <p className="text-sm text-gray-600">Refresh Interval</p>
                  <p className="text-sm font-mono mt-1">{config.agent_configuration.refresh_interval || 'Not set'}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-600">Watch Mode</p>
                  <p className="text-sm font-mono mt-1 uppercase">
                    {config.agent_configuration.watch_mode || 'Not set'}
                  </p>
                </div>
              </div>

              {config.agent_configuration.rotation_enabled && (
                <div className="pt-3 border-t">
                  <p className="text-sm font-medium text-gray-900">Rotation</p>
                  <div className="grid grid-cols-2 gap-4 mt-2">
                    <div>
                      <p className="text-xs text-gray-600">Strategy</p>
                      <Badge className="mt-1 bg-purple-100 text-purple-800">
                        {config.agent_configuration.rotation_strategy}
                      </Badge>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Providers Section */}
      {providers && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Server className="w-5 h-5" />
              Providers
            </CardTitle>
            <CardDescription>Configured secret providers and their status</CardDescription>
          </CardHeader>
          <CardContent>
            {Object.keys(providers.active).length === 0 ? (
              <p className="text-sm text-gray-600 py-4">No providers configured</p>
            ) : (
              <div className="space-y-3">
                {Object.entries(providers.active).map(([name, provider]) => {
                  const testResult = testResults[name]
                  const isTesting = testingProvider === name

                  return (
                    <div key={name} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
                      <div className="flex-1">
                        <p className="font-medium text-sm">{name}</p>
                        <p className="text-xs text-gray-600 capitalize">{provider.type}</p>
                      </div>

                      <div className="flex items-center gap-3">
                        {/* Test Result Status */}
                        {testResult && (
                          <div className="text-right">
                            {testResult.success ? (
                              <Badge className="bg-green-100 text-green-800 flex items-center gap-1">
                                <CheckCircle2 className="w-3 h-3" />
                                Connected
                              </Badge>
                            ) : (
                              <Badge variant="destructive" className="flex items-center gap-1">
                                <AlertCircle className="w-3 h-3" />
                                Failed
                              </Badge>
                            )}
                            {testResult.latency_ms && (
                              <p className="text-xs text-gray-600 mt-1">{testResult.latency_ms}ms</p>
                            )}
                          </div>
                        )}

                        {/* Test Button */}
                        <Button
                          onClick={() => testProvider(name)}
                          disabled={isTesting}
                          variant="outline"
                          size="sm"
                          className="whitespace-nowrap"
                        >
                          {isTesting ? (
                            <>
                              <RefreshCw className="w-3 h-3 mr-1 animate-spin" />
                              Testing...
                            </>
                          ) : (
                            'Test Connection'
                          )}
                        </Button>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Raw YAML Viewer */}
      <Card>
        <CardHeader>
          <CardTitle>Raw Configuration</CardTitle>
          <CardDescription>YAML configuration file contents</CardDescription>
        </CardHeader>
        <CardContent>
          <RawYAMLViewer />
        </CardContent>
      </Card>

      {/* Info Banner */}
      <div className="bg-blue-50 border border-blue-200 rounded-lg p-4">
        <p className="text-sm text-blue-900">
          <strong>Read-Only View:</strong> This is a read-only view of your configuration. To edit your
          configuration, use the CLI command{' '}
          <code className="bg-blue-100 px-2 py-1 rounded text-xs font-mono">dso config edit</code> or edit the
          file directly at <code className="bg-blue-100 px-2 py-1 rounded text-xs font-mono">/etc/dso/dso.yaml</code>.
        </p>
      </div>
    </div>
    </ErrorBoundary>
  )
}

// Raw YAML Viewer Component
function RawYAMLViewer() {
  const [yaml, setYaml] = useState<string>('')
  const [loadingYaml, setLoadingYaml] = useState(true)

  React.useEffect(() => {
    fetch('/api/config/raw')
      .then((r) => r.json())
      .then((data) => {
        setYaml(data.content || '')
        setLoadingYaml(false)
      })
      .catch((err) => {
        console.error('Failed to load YAML:', err)
        setLoadingYaml(false)
      })
  }, [])

  if (loadingYaml) {
    return <p className="text-sm text-gray-600">Loading...</p>
  }

  return (
    <div className="space-y-3">
      <div className="relative">
        <pre className="bg-gray-900 text-gray-100 p-4 rounded-lg overflow-x-auto text-sm font-mono whitespace-pre-wrap break-words">
          {yaml}
        </pre>
      </div>
      <Button
        onClick={() => {
          navigator.clipboard.writeText(yaml)
        }}
        variant="outline"
        size="sm"
      >
        Copy YAML
      </Button>
    </div>
  )
}
