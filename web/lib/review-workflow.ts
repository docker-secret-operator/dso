/**
 * Review & Approval Workflow
 *
 * Simulates a complete review and approval process for workspace drafts.
 * All data is browser-memory only. No persistence, no writes.
 */

import type { WorkspaceState } from './workspace'
import type { WorkspaceValidationResult } from './workspace-validation'

export type ApprovalStatus = 'draft' | 'under_review' | 'approved' | 'rejected' | 'expired'
export type RiskLevel = 'low' | 'medium' | 'high' | 'critical'

export interface ReviewChecklist {
  validationPassed: boolean
  noCriticalErrors: boolean
  noMissingDependencies: boolean
  noProviderConflicts: boolean
  operatorApproved: boolean
}

export interface RiskAssessment {
  score: number // 0-100
  level: RiskLevel
  factors: {
    affectedContainers: number
    affectedSecrets: number
    criticalValidationErrors: number
    highRiskChanges: number
  }
  explanation: string
}

export interface ReviewActivity {
  id: string
  type: 'created' | 'validation_performed' | 'status_changed' | 'exported' | 'commented'
  description: string
  timestamp: string
  metadata?: Record<string, any>
}

export interface DraftReview {
  id: string
  workspaceId: string
  status: ApprovalStatus
  createdAt: string
  createdBy: string
  title: string
  description: string
  checklist: ReviewChecklist
  riskAssessment: RiskAssessment
  activities: ReviewActivity[]
  mappingsAffected: number
  secretsAffected: number
  validationErrorCount: number
  validationWarningCount: number
  lastStatusChangeAt?: string
  expiresAt?: string
  approvedAt?: string
  rejectedAt?: string
  rejectionReason?: string
}

/**
 * Create a new draft review
 */
export function createDraftReview(
  workspace: WorkspaceState,
  validationResults: WorkspaceValidationResult[],
  title: string = 'Workspace Configuration Review',
  description: string = 'Review workspace changes before application'
): DraftReview {
  const checklist = generateChecklist(validationResults)
  const riskAssessment = calculateRiskAssessment(workspace, validationResults)

  const now = new Date().toISOString()

  return {
    id: `review-${Date.now()}`,
    workspaceId: workspace.config.id,
    status: 'draft',
    createdAt: now,
    createdBy: 'operator',
    title,
    description,
    checklist,
    riskAssessment,
    activities: [
      {
        id: `activity-${Date.now()}`,
        type: 'created',
        description: 'Draft review created',
        timestamp: now,
      },
    ],
    mappingsAffected: workspace.config.mappings.length,
    secretsAffected: workspace.config.secrets.length,
    validationErrorCount: validationResults.filter((r) => r.severity === 'error').length,
    validationWarningCount: validationResults.filter((r) => r.severity === 'warning').length,
  }
}

/**
 * Generate review checklist
 */
export function generateChecklist(validationResults: WorkspaceValidationResult[]): ReviewChecklist {
  const criticalErrors = validationResults.filter((r) => r.severity === 'error')
  const conflicts = validationResults.filter((r) => r.category === 'conflict')
  const missingDeps = validationResults.filter((r) => r.category === 'missing')

  return {
    validationPassed: validationResults.length === 0,
    noCriticalErrors: criticalErrors.length === 0,
    noMissingDependencies: missingDeps.length === 0,
    noProviderConflicts: conflicts.length === 0,
    operatorApproved: false, // Operator must explicitly approve
  }
}

/**
 * Calculate risk assessment
 */
export function calculateRiskAssessment(
  workspace: WorkspaceState,
  validationResults: WorkspaceValidationResult[]
): RiskAssessment {
  const affectedContainers = new Set(
    workspace.config.mappings.map((m) => m.container)
  ).size
  const affectedSecrets = workspace.config.secrets.length
  const criticalErrors = validationResults.filter((r) => r.severity === 'error').length
  const highRiskChanges = workspace.config.mappings.filter(
    (m) => /password|secret|token|key|auth/i.test(m.secret)
  ).length

  // Calculate score (0-100)
  let score = 0

  // Base from affected resources (0-40)
  score += Math.min((affectedContainers / 10) * 20, 20) // containers
  score += Math.min((affectedSecrets / 10) * 20, 20) // secrets

  // Validation errors (0-30)
  score += Math.min(criticalErrors * 10, 30)

  // High-risk changes (0-30)
  score += Math.min((highRiskChanges / 5) * 30, 30)

  // Determine level
  let level: RiskLevel
  if (score >= 80) {
    level = 'critical'
  } else if (score >= 60) {
    level = 'high'
  } else if (score >= 40) {
    level = 'medium'
  } else {
    level = 'low'
  }

  // Generate explanation
  const factors: string[] = []
  if (affectedContainers > 5) factors.push(`${affectedContainers} containers affected`)
  if (affectedSecrets > 3) factors.push(`${affectedSecrets} secrets affected`)
  if (criticalErrors > 0) factors.push(`${criticalErrors} validation errors`)
  if (highRiskChanges > 0) factors.push(`${highRiskChanges} security-sensitive changes`)

  const explanation =
    factors.length > 0
      ? `High scope: ${factors.join(', ')}`
      : 'Limited scope with no critical issues detected'

  return {
    score: Math.round(score),
    level,
    factors: {
      affectedContainers,
      affectedSecrets,
      criticalValidationErrors: criticalErrors,
      highRiskChanges,
    },
    explanation,
  }
}

/**
 * Change review status
 */
export function changeReviewStatus(
  review: DraftReview,
  newStatus: ApprovalStatus,
  reason?: string
): DraftReview {
  const now = new Date().toISOString()
  const activity: ReviewActivity = {
    id: `activity-${Date.now()}`,
    type: 'status_changed',
    description: `Status changed from ${review.status} to ${newStatus}`,
    timestamp: now,
  }

  const updated: DraftReview = {
    ...review,
    status: newStatus,
    activities: [...review.activities, activity],
    lastStatusChangeAt: now,
  }

  if (newStatus === 'approved') {
    updated.approvedAt = now
  } else if (newStatus === 'rejected' && reason) {
    updated.rejectedAt = now
    updated.rejectionReason = reason
  } else if (newStatus === 'expired') {
    updated.expiresAt = now
  }

  return updated
}

/**
 * Update operator approval
 */
export function updateOperatorApproval(
  review: DraftReview,
  approved: boolean
): DraftReview {
  const now = new Date().toISOString()

  return {
    ...review,
    checklist: {
      ...review.checklist,
      operatorApproved: approved,
    },
    activities: [
      ...review.activities,
      {
        id: `activity-${Date.now()}`,
        type: 'commented',
        description: approved ? 'Operator approved the review' : 'Operator rejected the review',
        timestamp: now,
      },
    ],
  }
}

/**
 * Add activity to review
 */
export function addActivityToReview(
  review: DraftReview,
  type: ReviewActivity['type'],
  description: string,
  metadata?: Record<string, any>
): DraftReview {
  return {
    ...review,
    activities: [
      ...review.activities,
      {
        id: `activity-${Date.now()}`,
        type,
        description,
        timestamp: new Date().toISOString(),
        metadata,
      },
    ],
  }
}

/**
 * Check if all checklist items pass
 */
export function isChecklistComplete(checklist: ReviewChecklist): boolean {
  return (
    checklist.validationPassed &&
    checklist.noCriticalErrors &&
    checklist.noMissingDependencies &&
    checklist.noProviderConflicts &&
    checklist.operatorApproved
  )
}

/**
 * Can approve (all checks pass except operator approval)
 */
export function canApprove(review: DraftReview): boolean {
  return (
    review.checklist.noCriticalErrors &&
    review.checklist.noMissingDependencies &&
    review.checklist.noProviderConflicts &&
    review.checklist.operatorApproved
  )
}

/**
 * Export review as JSON
 */
export function exportReviewAsJSON(review: DraftReview): string {
  return JSON.stringify(
    {
      reviewId: review.id,
      status: review.status,
      createdAt: review.createdAt,
      title: review.title,
      description: review.description,
      summary: {
        mappingsAffected: review.mappingsAffected,
        secretsAffected: review.secretsAffected,
        validationErrors: review.validationErrorCount,
        validationWarnings: review.validationWarningCount,
      },
      checklist: review.checklist,
      riskAssessment: review.riskAssessment,
      timeline: review.activities.map((a) => ({
        timestamp: a.timestamp,
        action: a.type,
        description: a.description,
      })),
    },
    null,
    2
  )
}

/**
 * Export review as YAML
 */
export function exportReviewAsYAML(review: DraftReview): string {
  const json = JSON.parse(exportReviewAsJSON(review))

  let yaml = `reviewId: ${json.reviewId}\n`
  yaml += `status: ${json.status}\n`
  yaml += `createdAt: ${json.createdAt}\n`
  yaml += `title: ${json.title}\n`
  yaml += `description: ${json.description}\n`
  yaml += `\nsummary:\n`
  yaml += `  mappingsAffected: ${json.summary.mappingsAffected}\n`
  yaml += `  secretsAffected: ${json.summary.secretsAffected}\n`
  yaml += `  validationErrors: ${json.summary.validationErrors}\n`
  yaml += `  validationWarnings: ${json.summary.validationWarnings}\n`
  yaml += `\nchecklist:\n`
  yaml += `  validationPassed: ${json.checklist.validationPassed}\n`
  yaml += `  noCriticalErrors: ${json.checklist.noCriticalErrors}\n`
  yaml += `  noMissingDependencies: ${json.checklist.noMissingDependencies}\n`
  yaml += `  noProviderConflicts: ${json.checklist.noProviderConflicts}\n`
  yaml += `  operatorApproved: ${json.checklist.operatorApproved}\n`
  yaml += `\nriskAssessment:\n`
  yaml += `  score: ${json.riskAssessment.score}\n`
  yaml += `  level: ${json.riskAssessment.level}\n`
  yaml += `  explanation: ${json.riskAssessment.explanation}\n`
  yaml += `\ntimeline:\n`
  json.timeline.forEach((event: any, i: number) => {
    yaml += `  - timestamp: ${event.timestamp}\n`
    yaml += `    action: ${event.action}\n`
    yaml += `    description: ${event.description}\n`
  })

  return yaml
}

/**
 * Download review report
 */
export function downloadReviewReport(review: DraftReview, format: 'json' | 'yaml') {
  const content = format === 'json' ? exportReviewAsJSON(review) : exportReviewAsYAML(review)
  const filename = `review-${review.id}-${new Date().toISOString().split('T')[0]}.${format}`

  const blob = new Blob([content], {
    type: format === 'json' ? 'application/json' : 'text/yaml',
  })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

/**
 * Generate review summary statistics
 */
export function generateReviewSummary(reviews: DraftReview[]) {
  return {
    total: reviews.length,
    draft: reviews.filter((r) => r.status === 'draft').length,
    underReview: reviews.filter((r) => r.status === 'under_review').length,
    approved: reviews.filter((r) => r.status === 'approved').length,
    rejected: reviews.filter((r) => r.status === 'rejected').length,
    expired: reviews.filter((r) => r.status === 'expired').length,
    riskCritical: reviews.filter((r) => r.riskAssessment.level === 'critical').length,
    riskHigh: reviews.filter((r) => r.riskAssessment.level === 'high').length,
    riskMedium: reviews.filter((r) => r.riskAssessment.level === 'medium').length,
    riskLow: reviews.filter((r) => r.riskAssessment.level === 'low').length,
  }
}
