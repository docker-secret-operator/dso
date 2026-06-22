'use client'

import { useState, useMemo, useEffect } from 'react'
import { apiFetch } from "@/lib/api-fetch"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ErrorBoundary } from '@/components/error-boundary'
import { Download, CheckCircle2, XCircle, AlertCircle, AlertTriangle, Clock, Plus } from 'lucide-react'
import { useRouter } from 'next/navigation'
import {
  createDraftReview,
  changeReviewStatus,
  updateOperatorApproval,
  canApprove,
  downloadReviewReport,
  generateReviewSummary,
  type DraftReview,
  type ApprovalStatus,
} from '@/lib/review-workflow'
import { createWorkspace, getWorkspaceSummary } from '@/lib/workspace'
import { validateDraftConfiguration, generateValidationSummary, sortValidationResults } from '@/lib/workspace-validation'
import { useQuery } from '@tanstack/react-query'

export function ReviewPageClient() {
  const router = useRouter()
  const [reviews, setReviews] = useState<DraftReview[]>([])
  const [selectedReview, setSelectedReview] = useState<DraftReview | null>(null)
  const [selectedFormat, setSelectedFormat] = useState<'json' | 'yaml'>('json')
  const [operatorApprovalGiven, setOperatorApprovalGiven] = useState(false)

  const { data: containers = [] } = useQuery({
    queryKey: ['containers'],
    queryFn: async () => {
      try {
        const response = await apiFetch('/api/discovery/docker')
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
        const response = await apiFetch('/api/secrets')
        if (!response.ok) return []
        const data = await response.json()
        return data.secrets || []
      } catch {
        return []
      }
    },
  })

  // Auto-load review from workspace on mount
  useEffect(() => {
    const workspaceData = sessionStorage.getItem('workspace-for-review')
    if (workspaceData && reviews.length === 0) {
      try {
        const data = JSON.parse(workspaceData)
        const review = createDraftReview(
          data.workspace,
          data.validationResults || [],
          'Configuration Review from Workspace',
          'Review workspace changes before application'
        )
        setReviews([review])
        setSelectedReview(review)
        sessionStorage.removeItem('workspace-for-review')
      } catch (error) {
        console.error('Failed to load review from workspace:', error)
      }
    }
  }, [])

  // Create a review from workspace
  const handleCreateReviewFromWorkspace = () => {
    const workspace = createWorkspace()
    const mappings = containers
      .flatMap((c: any) => (c.dso_awareness?.managed_secrets || []).map((s: string) => ({ container: c.name, secret: s })))

    const validationResults = sortValidationResults(
      validateDraftConfiguration(workspace, containers, secrets, mappings)
    )

    const review = createDraftReview(
      workspace,
      validationResults,
      'Configuration Review',
      'Review and approve configuration changes'
    )

    setReviews([...reviews, review])
    setSelectedReview(review)
  }

  const handleChangeStatus = (newStatus: ApprovalStatus) => {
    if (!selectedReview) return

    let updated = changeReviewStatus(selectedReview, newStatus)
    if (newStatus === 'rejected') {
      updated = changeReviewStatus(updated, newStatus, 'Operator rejected this review')
    }

    setSelectedReview(updated)
    setReviews(reviews.map((r) => (r.id === updated.id ? updated : r)))
  }

  const handleApproveClick = () => {
    if (!selectedReview) return
    setOperatorApprovalGiven(!operatorApprovalGiven)
    let updated = updateOperatorApproval(selectedReview, !operatorApprovalGiven)
    setSelectedReview(updated)
    setReviews(reviews.map((r) => (r.id === updated.id ? updated : r)))
  }

  const handleDownload = () => {
    if (!selectedReview) return
    downloadReviewReport(selectedReview, selectedFormat)
  }

  const summary = useMemo(() => generateReviewSummary(reviews), [reviews])

  const getRiskColor = (level: string) => {
    switch (level) {
      case 'critical':
        return 'bg-red-100 text-red-800'
      case 'high':
        return 'bg-orange-100 text-orange-800'
      case 'medium':
        return 'bg-yellow-100 text-yellow-800'
      default:
        return 'bg-green-100 text-green-800'
    }
  }

  const getRiskIcon = (level: string) => {
    switch (level) {
      case 'critical':
      case 'high':
        return <AlertCircle className="w-4 h-4" />
      case 'medium':
        return <AlertTriangle className="w-4 h-4" />
      default:
        return <CheckCircle2 className="w-4 h-4" />
    }
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'approved':
        return <CheckCircle2 className="w-4 h-4 text-green-600" />
      case 'rejected':
        return <XCircle className="w-4 h-4 text-red-600" />
      case 'under_review':
        return <Clock className="w-4 h-4 text-blue-600" />
      default:
        return <AlertCircle className="w-4 h-4 text-yellow-600" />
    }
  }

  const getStatusBadgeColor = (status: string) => {
    switch (status) {
      case 'approved':
        return 'bg-green-100 text-green-800'
      case 'rejected':
        return 'bg-red-100 text-red-800'
      case 'under_review':
        return 'bg-blue-100 text-blue-800'
      default:
        return 'bg-yellow-100 text-yellow-800'
    }
  }

  return (
    <ErrorBoundary>
      <div className="p-8 space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold">Review & Approval Workflow</h1>
            <p className="text-gray-600 mt-1">Simulate review and approval of workspace configurations</p>
          </div>
          <Button onClick={handleCreateReviewFromWorkspace} className="gap-2">
            <Plus className="w-4 h-4" />
            Create Review
          </Button>
        </div>

        {/* Summary Cards */}
        <div className="grid grid-cols-4 gap-4">
          <Card>
            <CardContent className="pt-6">
              <p className="text-3xl font-bold">{summary.total}</p>
              <p className="text-xs text-gray-600 mt-1">Total Reviews</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-3xl font-bold text-yellow-600">{summary.draft}</p>
              <p className="text-xs text-gray-600 mt-1">Draft</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-3xl font-bold text-green-600">{summary.approved}</p>
              <p className="text-xs text-gray-600 mt-1">Approved</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-3xl font-bold text-red-600">{summary.rejected}</p>
              <p className="text-xs text-gray-600 mt-1">Rejected</p>
            </CardContent>
          </Card>
        </div>

        {/* Risk Distribution */}
        <div className="grid grid-cols-4 gap-4">
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold text-red-600">{summary.riskCritical}</p>
              <p className="text-xs text-gray-600 mt-1">Critical Risk</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold text-orange-600">{summary.riskHigh}</p>
              <p className="text-xs text-gray-600 mt-1">High Risk</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold text-yellow-600">{summary.riskMedium}</p>
              <p className="text-xs text-gray-600 mt-1">Medium Risk</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-2xl font-bold text-green-600">{summary.riskLow}</p>
              <p className="text-xs text-gray-600 mt-1">Low Risk</p>
            </CardContent>
          </Card>
        </div>

        <div className="grid grid-cols-3 gap-6">
          {/* Reviews List */}
          <div className="col-span-1">
            <Card>
              <CardHeader>
                <CardTitle>Reviews</CardTitle>
                <CardDescription>All review history (browser memory)</CardDescription>
              </CardHeader>
              <CardContent className="space-y-2 max-h-96 overflow-y-auto">
                {reviews.length === 0 ? (
                  <p className="text-sm text-gray-600">No reviews created yet</p>
                ) : (
                  reviews.map((review) => (
                    <button
                      key={review.id}
                      onClick={() => setSelectedReview(review)}
                      className={`w-full text-left p-3 rounded border transition-colors ${
                        selectedReview?.id === review.id
                          ? 'bg-blue-50 border-blue-300'
                          : 'bg-white border-gray-200 hover:bg-gray-50'
                      }`}
                    >
                      <div className="flex items-center gap-2 mb-1">
                        {getStatusIcon(review.status)}
                        <p className="text-sm font-semibold truncate">{review.title}</p>
                      </div>
                      <div className="flex items-center gap-2 text-xs">
                        <Badge variant="outline" className={`text-xs ${getRiskColor(review.riskAssessment.level)}`}>
                          Risk: {review.riskAssessment.score}
                        </Badge>
                        <Badge variant="outline" className={`text-xs ${getStatusBadgeColor(review.status)}`}>
                          {review.status.replace('_', ' ')}
                        </Badge>
                      </div>
                    </button>
                  ))
                )}
              </CardContent>
            </Card>
          </div>

          {/* Review Details */}
          <div className="col-span-2 space-y-4">
            {selectedReview ? (
              <>
                {/* Header */}
                <Card>
                  <CardHeader>
                    <div className="flex items-center justify-between">
                      <div>
                        <CardTitle>{selectedReview.title}</CardTitle>
                        <CardDescription>{selectedReview.description}</CardDescription>
                      </div>
                      <div className="flex items-center gap-2">
                        {getStatusIcon(selectedReview.status)}
                        <Badge className={`text-sm ${getStatusBadgeColor(selectedReview.status)}`}>
                          {selectedReview.status.replace('_', ' ')}
                        </Badge>
                      </div>
                    </div>
                  </CardHeader>
                </Card>

                {/* Risk Assessment */}
                <Card>
                  <CardHeader>
                    <CardTitle>Risk Assessment</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <div className="flex items-center gap-4">
                      <div className="flex-1">
                        <p className="text-sm text-gray-600 mb-2">Risk Score</p>
                        <div className="w-full h-2 bg-gray-200 rounded-full overflow-hidden">
                          <div
                            className={`h-full ${
                              selectedReview.riskAssessment.score >= 80
                                ? 'bg-red-600'
                                : selectedReview.riskAssessment.score >= 60
                                  ? 'bg-orange-600'
                                  : selectedReview.riskAssessment.score >= 40
                                    ? 'bg-yellow-600'
                                    : 'bg-green-600'
                            }`}
                            style={{ width: `${selectedReview.riskAssessment.score}%` }}
                          />
                        </div>
                        <div className="flex justify-between mt-1 text-xs text-gray-600">
                          <span>0</span>
                          <span className="font-semibold">{selectedReview.riskAssessment.score}</span>
                          <span>100</span>
                        </div>
                      </div>
                      <Badge className={`text-sm ${getRiskColor(selectedReview.riskAssessment.level)} px-3 py-1`}>
                        {getRiskIcon(selectedReview.riskAssessment.level)}
                        {selectedReview.riskAssessment.level.toUpperCase()}
                      </Badge>
                    </div>
                    <div className="space-y-2 text-sm">
                      <p className="text-gray-700">{selectedReview.riskAssessment.explanation}</p>
                      <div className="grid grid-cols-2 gap-2 pt-2">
                        <div className="p-2 bg-gray-50 rounded">
                          <p className="text-xs text-gray-600">Containers Affected</p>
                          <p className="text-lg font-semibold">{selectedReview.riskAssessment.factors.affectedContainers}</p>
                        </div>
                        <div className="p-2 bg-gray-50 rounded">
                          <p className="text-xs text-gray-600">Secrets Affected</p>
                          <p className="text-lg font-semibold">{selectedReview.riskAssessment.factors.affectedSecrets}</p>
                        </div>
                        <div className="p-2 bg-gray-50 rounded">
                          <p className="text-xs text-gray-600">Validation Errors</p>
                          <p className="text-lg font-semibold">{selectedReview.riskAssessment.factors.criticalValidationErrors}</p>
                        </div>
                        <div className="p-2 bg-gray-50 rounded">
                          <p className="text-xs text-gray-600">Security Changes</p>
                          <p className="text-lg font-semibold">{selectedReview.riskAssessment.factors.highRiskChanges}</p>
                        </div>
                      </div>
                    </div>
                  </CardContent>
                </Card>

                {/* Review Checklist */}
                <Card>
                  <CardHeader>
                    <CardTitle>Review Checklist</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    {[
                      { label: 'Validation Passed', value: selectedReview.checklist.validationPassed },
                      { label: 'No Critical Errors', value: selectedReview.checklist.noCriticalErrors },
                      { label: 'No Missing Dependencies', value: selectedReview.checklist.noMissingDependencies },
                      { label: 'No Provider Conflicts', value: selectedReview.checklist.noProviderConflicts },
                      { label: 'Operator Approval', value: selectedReview.checklist.operatorApproved },
                    ].map((item, i) => (
                      <div key={i} className="flex items-center gap-3 p-2 rounded hover:bg-gray-50">
                        {item.value ? (
                          <CheckCircle2 className="w-5 h-5 text-green-600 flex-shrink-0" />
                        ) : (
                          <XCircle className="w-5 h-5 text-red-600 flex-shrink-0" />
                        )}
                        <p className={`text-sm ${item.value ? 'text-gray-900' : 'text-gray-600'}`}>{item.label}</p>
                      </div>
                    ))}
                  </CardContent>
                </Card>

                {/* Validation Summary */}
                <Card>
                  <CardHeader>
                    <CardTitle>Validation Summary</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-2 text-sm">
                    <div className="flex justify-between p-2 bg-gray-50 rounded">
                      <span>Mappings Affected</span>
                      <span className="font-semibold">{selectedReview.mappingsAffected}</span>
                    </div>
                    <div className="flex justify-between p-2 bg-gray-50 rounded">
                      <span>Secrets Affected</span>
                      <span className="font-semibold">{selectedReview.secretsAffected}</span>
                    </div>
                    <div className="flex justify-between p-2 bg-red-50 rounded">
                      <span className="text-red-700">Validation Errors</span>
                      <span className="font-semibold text-red-700">{selectedReview.validationErrorCount}</span>
                    </div>
                    <div className="flex justify-between p-2 bg-yellow-50 rounded">
                      <span className="text-yellow-700">Validation Warnings</span>
                      <span className="font-semibold text-yellow-700">{selectedReview.validationWarningCount}</span>
                    </div>
                  </CardContent>
                </Card>

                {/* Actions */}
                <Card>
                  <CardHeader>
                    <CardTitle>Actions</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    {/* Operator Approval */}
                    <div className="space-y-2">
                      <p className="text-sm font-semibold">Operator Approval</p>
                      <Button
                        variant={operatorApprovalGiven ? 'default' : 'outline'}
                        className="w-full"
                        onClick={handleApproveClick}
                      >
                        {operatorApprovalGiven ? '✓ Approved by Operator' : 'Approve as Operator'}
                      </Button>
                    </div>

                    {/* Status Changes */}
                    <div className="space-y-2">
                      <p className="text-sm font-semibold">Review Status</p>
                      <div className="grid grid-cols-2 gap-2">
                        <Button
                          variant={selectedReview.status === 'under_review' ? 'default' : 'outline'}
                          size="sm"
                          onClick={() => handleChangeStatus('under_review')}
                          className="text-xs"
                        >
                          Under Review
                        </Button>
                        <Button
                          variant={selectedReview.status === 'approved' ? 'default' : 'outline'}
                          size="sm"
                          onClick={() => handleChangeStatus('approved')}
                          className="text-xs"
                          disabled={!canApprove(selectedReview)}
                        >
                          Approve
                        </Button>
                        <Button
                          variant={selectedReview.status === 'rejected' ? 'destructive' : 'outline'}
                          size="sm"
                          onClick={() => handleChangeStatus('rejected')}
                          className="text-xs"
                        >
                          Reject
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleChangeStatus('expired')}
                          className="text-xs"
                        >
                          Mark Expired
                        </Button>
                      </div>
                    </div>

                    {/* Export */}
                    <div className="space-y-2">
                      <p className="text-sm font-semibold">Export Report</p>
                      <div className="flex gap-2">
                        {(['json', 'yaml'] as const).map((format) => (
                          <button
                            key={format}
                            onClick={() => setSelectedFormat(format)}
                            className={`flex-1 px-2 py-1 text-sm rounded border transition-colors ${
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
                        Download Report
                      </Button>
                    </div>
                  </CardContent>
                </Card>

                {/* Timeline */}
                <Card>
                  <CardHeader>
                    <CardTitle>Review Timeline</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-2 max-h-64 overflow-y-auto">
                    {selectedReview.activities.map((activity, i) => (
                      <div key={activity.id} className="text-sm pb-2 border-b last:border-b-0">
                        <p className="font-semibold text-gray-900">{activity.description}</p>
                        <p className="text-xs text-gray-600">
                          {new Date(activity.timestamp).toLocaleString()}
                        </p>
                      </div>
                    ))}
                  </CardContent>
                </Card>
              </>
            ) : (
              <Card className="col-span-2 flex items-center justify-center h-96">
                <div className="text-center">
                  <p className="text-gray-600 mb-4">Select a review or create a new one to see details</p>
                  <Button onClick={handleCreateReviewFromWorkspace}>Create First Review</Button>
                </div>
              </Card>
            )}
          </div>
        </div>

        {/* Info Banner */}
        <Card className="bg-blue-50 border-blue-200">
          <CardContent className="pt-6">
            <p className="text-sm text-blue-900">
              <strong>Browser Memory Only:</strong> All reviews exist only in browser memory. No data is persisted.
              Page refresh will clear all reviews. This is a simulation of the approval workflow.
            </p>
          </CardContent>
        </Card>
      </div>
    </ErrorBoundary>
  )
}
