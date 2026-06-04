'use client'

import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ErrorBoundary } from '@/components/error-boundary'
import { Download, Trash2, RefreshCw, AlertCircle, AlertTriangle, Info, FileCheck } from 'lucide-react'
import { useState, useMemo, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import {
  createWorkspace,
  initializeWorkspaceFromCurrent,
  addMapping,
  removeMapping,
  addSecret,
  getWorkspaceSummary,
  getConfigDiff,
  downloadDraft,
  type WorkspaceState,
} from '@/lib/workspace'
import {
  validateDraftConfiguration,
  generateValidationSummary,
  sortValidationResults,
  type WorkspaceValidationResult,
} from '@/lib/workspace-validation'

export function WorkspacePageClient() {
  const router = useRouter()
  const [workspace, setWorkspace] = useState<WorkspaceState>(() => createWorkspace())
  const [selectedFormat, setSelectedFormat] = useState<'json' | 'yaml'>('json')
  const [hasMergedDraft, setHasMergedDraft] = useState(false)

  const { data: containers = [] } = useQuery({
    queryKey: ['containers'],
    queryFn: async () => {
      try {
        const response = await fetch('/api/discovery/docker')
        if (!response.ok) return []
        const data = await response.json()
        return data.containers || []
      } catch {
        return []
      }
    },
  })

  const { data: secrets = [] } = useQuery({
    queryKey: ['secrets'],
    queryFn: async () => {
      try {
        const response = await fetch('/api/secrets')
        if (!response.ok) return []
        const data = await response.json()
        return data.secrets || []
      } catch {
        return []
      }
    },
  })

  const mappings = useMemo(() => {
    const result: Array<{ container: string; secret: string }> = []
    containers.forEach((container: any) => {
      const containerSecrets = container.dso_awareness?.managed_secrets || []
      containerSecrets.forEach((secret: string) => {
        result.push({ container: container.name, secret })
      })
    })
    return result
  }, [containers])

  const summary = useMemo(() => getWorkspaceSummary(workspace), [workspace])

  const diff = useMemo(() => {
    return getConfigDiff(
      mappings.map((m) => ({ container: m.container, secret: m.secret })),
      workspace.config.mappings.map((m) => ({ container: m.container, secret: m.secret }))
    )
  }, [mappings, workspace.config.mappings])

  const validationResults = useMemo(() => {
    const results = validateDraftConfiguration(
      workspace,
      containers,
      secrets,
      mappings
    )
    return sortValidationResults(results)
  }, [workspace, containers, secrets, mappings])

  const validationSummary = useMemo(() => {
    return generateValidationSummary(validationResults)
  }, [validationResults])

  const handleAddMapping = (container: string, secret: string) => {
    setWorkspace((ws) => addMapping(ws, container, secret))
  }

  const handleRemoveMapping = (mappingId: string) => {
    setWorkspace((ws) => removeMapping(ws, mappingId))
  }

  const handleClear = () => {
    setWorkspace(() => createWorkspace())
  }

  const handleReset = () => {
    setWorkspace(() => initializeWorkspaceFromCurrent(containers, secrets, mappings))
  }

  const handleDownload = () => {
    downloadDraft(workspace, selectedFormat)
  }

  const handleCreateReview = () => {
    // Store workspace data in sessionStorage for review page to pick up
    sessionStorage.setItem(
      'workspace-for-review',
      JSON.stringify({
        workspace,
        validationResults: validationResults,
        timestamp: new Date().toISOString(),
      })
    )
    router.push('/review')
  }

  // Apply draft from drift issue, remediation plan, or change set
  useEffect(() => {
    if (hasMergedDraft) return

    const fromIssue = sessionStorage.getItem('workspace-draft-from-issue')
    const fromPlan = sessionStorage.getItem('workspace-draft-from-plan')
    const fromChangeSet = sessionStorage.getItem('workspace-draft-from-changeset')

    if (fromIssue || fromPlan || fromChangeSet) {
      try {
        const data = fromIssue ? JSON.parse(fromIssue) : fromPlan ? JSON.parse(fromPlan) : JSON.parse(fromChangeSet!)

        let updatedWorkspace = workspace
        if (data.changes && Array.isArray(data.changes)) {
          data.changes.forEach((change: any) => {
            if (change.type === 'add_mapping' && change.container && change.secret) {
              updatedWorkspace = addMapping(updatedWorkspace, change.container, change.secret)
            } else if (change.type === 'remove_mapping' && change.container && change.secret) {
              updatedWorkspace = removeMapping(updatedWorkspace, `${change.container}-${change.secret}`)
            } else if (change.type === 'add_secret' && change.secret) {
              updatedWorkspace = addSecret(updatedWorkspace, change.secret, 'vault')
            }
          })
        }

        setWorkspace(updatedWorkspace)
        setHasMergedDraft(true)

        // Clear sessionStorage
        sessionStorage.removeItem('workspace-draft-from-issue')
        sessionStorage.removeItem('workspace-draft-from-plan')
        sessionStorage.removeItem('workspace-draft-from-changeset')
      } catch (error) {
        console.error('Failed to merge draft:', error)
        setHasMergedDraft(true)
      }
    }
  }, [hasMergedDraft, workspace])

  return (
    <ErrorBoundary>
      <div className="p-8 space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold">Configuration Workspace</h1>
            <p className="text-gray-600 mt-1">
              Simulate configuration changes in browser memory
            </p>
          </div>
          <div className="flex gap-2">
            <Button onClick={handleReset} variant="outline" size="sm" className="gap-2">
              <RefreshCw className="w-4 h-4" />
              Reset
            </Button>
            <Button onClick={handleClear} variant="outline" size="sm" className="gap-2">
              <Trash2 className="w-4 h-4" />
              Clear
            </Button>
          </div>
        </div>

        {/* Summary */}
        <div className="grid grid-cols-5 gap-4">
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold">{summary.mappingCount}</p>
              <p className="text-xs text-gray-600 mt-1">Mappings</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold text-green-600">{summary.addedMappings}</p>
              <p className="text-xs text-gray-600 mt-1">Added</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold text-red-600">{summary.removedMappings}</p>
              <p className="text-xs text-gray-600 mt-1">Removed</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold">{summary.secretCount}</p>
              <p className="text-xs text-gray-600 mt-1">Secrets</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold">{summary.changeCount}</p>
              <p className="text-xs text-gray-600 mt-1">Changes</p>
            </CardContent>
          </Card>
        </div>

        {/* Validation Summary */}
        {validationResults.length > 0 && (
          <Card className={validationSummary.hasCriticalIssues ? 'border-red-200 bg-red-50' : 'border-yellow-200 bg-yellow-50'}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>Validation Results</CardTitle>
                <div className="flex gap-2">
                  {validationSummary.errors > 0 && (
                    <Badge variant="destructive" className="gap-1">
                      <AlertCircle className="w-3 h-3" />
                      {validationSummary.errors} Error{validationSummary.errors !== 1 ? 's' : ''}
                    </Badge>
                  )}
                  {validationSummary.warnings > 0 && (
                    <Badge variant="outline" className="border-yellow-300 bg-yellow-100 text-yellow-900 gap-1">
                      <AlertTriangle className="w-3 h-3" />
                      {validationSummary.warnings} Warning{validationSummary.warnings !== 1 ? 's' : ''}
                    </Badge>
                  )}
                  {validationSummary.infos > 0 && (
                    <Badge variant="outline" className="border-blue-300 bg-blue-100 text-blue-900 gap-1">
                      <Info className="w-3 h-3" />
                      {validationSummary.infos} Info
                    </Badge>
                  )}
                </div>
              </div>
              <CardDescription>
                Draft configuration validation against drift detection rules
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 max-h-96 overflow-y-auto">
              {validationResults.map((result) => (
                <div
                  key={result.id}
                  className={`p-3 rounded border text-sm ${
                    result.severity === 'error'
                      ? 'bg-red-100 border-red-300'
                      : result.severity === 'warning'
                        ? 'bg-yellow-100 border-yellow-300'
                        : 'bg-blue-100 border-blue-300'
                  }`}
                >
                  <div className="flex items-start gap-2">
                    {result.severity === 'error' ? (
                      <AlertCircle className="w-4 h-4 text-red-600 flex-shrink-0 mt-0.5" />
                    ) : result.severity === 'warning' ? (
                      <AlertTriangle className="w-4 h-4 text-yellow-600 flex-shrink-0 mt-0.5" />
                    ) : (
                      <Info className="w-4 h-4 text-blue-600 flex-shrink-0 mt-0.5" />
                    )}
                    <div className="flex-1">
                      <p
                        className={`font-semibold ${
                          result.severity === 'error'
                            ? 'text-red-900'
                            : result.severity === 'warning'
                              ? 'text-yellow-900'
                              : 'text-blue-900'
                        }`}
                      >
                        {result.message}
                      </p>
                      {result.details && (
                        <p
                          className={`text-xs mt-1 ${
                            result.severity === 'error'
                              ? 'text-red-700'
                              : result.severity === 'warning'
                                ? 'text-yellow-700'
                                : 'text-blue-700'
                          }`}
                        >
                          {result.details}
                        </p>
                      )}
                      {result.suggestedFix && (
                        <p className="text-xs mt-2 font-semibold text-gray-700">
                          💡 {result.suggestedFix}
                        </p>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </CardContent>
          </Card>
        )}

        {/* Export & Review */}
        <div className="grid grid-cols-2 gap-4">
          <Card>
            <CardHeader>
              <CardTitle>Export Draft</CardTitle>
              <CardDescription>Download draft configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex gap-2">
                {(['json', 'yaml'] as const).map((format) => (
                  <button
                    key={format}
                    onClick={() => setSelectedFormat(format)}
                    className={`px-3 py-2 text-sm rounded border transition-colors ${
                      selectedFormat === format
                        ? 'bg-blue-100 border-blue-300'
                        : 'bg-white border-gray-200 hover:bg-gray-50'
                    }`}
                  >
                    {format.toUpperCase()}
                  </button>
                ))}
              </div>
              <Button onClick={handleDownload} className="w-full gap-2">
                <Download className="w-4 h-4" />
                Download
              </Button>
              <p className="text-sm text-gray-600">
                Export draft configuration for review or external processing.
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Create Review</CardTitle>
              <CardDescription>Start approval workflow</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <Button onClick={handleCreateReview} className="w-full gap-2" variant="outline">
                <FileCheck className="w-4 h-4" />
                Create Review
              </Button>
              <p className="text-sm text-gray-600">
                Start a review and approval workflow for this draft configuration.
              </p>
            </CardContent>
          </Card>
        </div>

        {/* Diff View */}
        {workspace.isModified && (
          <Card className="border-blue-200 bg-blue-50">
            <CardHeader>
              <CardTitle>Changes Summary</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-3 gap-4">
                <div>
                  <p className="text-sm font-semibold text-green-900 mb-2">Added</p>
                  {diff.added.length > 0 ? (
                    <div className="space-y-1 text-sm">
                      {diff.added.slice(0, 5).map((item, i) => (
                        <p key={i} className="text-green-700">
                          + {item.container ? `${item.container}→${item.secret}` : item.name}
                        </p>
                      ))}
                      {diff.added.length > 5 && (
                        <p className="text-green-600 text-xs">+{diff.added.length - 5} more</p>
                      )}
                    </div>
                  ) : (
                    <p className="text-sm text-gray-600">No additions</p>
                  )}
                </div>
                <div>
                  <p className="text-sm font-semibold text-red-900 mb-2">Removed</p>
                  {diff.removed.length > 0 ? (
                    <div className="space-y-1 text-sm">
                      {diff.removed.slice(0, 5).map((item, i) => (
                        <p key={i} className="text-red-700">
                          - {item.container ? `${item.container}→${item.secret}` : item.name}
                        </p>
                      ))}
                      {diff.removed.length > 5 && (
                        <p className="text-red-600 text-xs">+{diff.removed.length - 5} more</p>
                      )}
                    </div>
                  ) : (
                    <p className="text-sm text-gray-600">No removals</p>
                  )}
                </div>
                <div>
                  <p className="text-sm font-semibold text-gray-900 mb-2">Unchanged</p>
                  <p className="text-sm text-gray-700">{diff.unchanged.length} items</p>
                </div>
              </div>
            </CardContent>
          </Card>
        )}

        {/* Info */}
        <Card className="bg-blue-50 border-blue-200">
          <CardContent className="pt-6">
            <p className="text-sm text-blue-900">
              <strong>Draft Workspace:</strong> All changes exist only in browser memory. Nothing is
              saved or applied to the system. Use Export to save your draft for review.
            </p>
          </CardContent>
        </Card>
      </div>
    </ErrorBoundary>
  )
}
