/**
 * Remediation Planner
 *
 * Converts drift issues into actionable remediation plans with:
 * - Impact estimation
 * - Risk assessment
 * - Priority scoring (0-100)
 * - Affected resource tracking
 *
 * All functions are pure and fully memoizable.
 */

import { DriftIssue, DriftSeverity } from './drift-detection'

export type RiskLevel = 'low' | 'medium' | 'high'
export type RemediationType =
  | 'add_mapping'
  | 'remove_mapping'
  | 'add_secret'
  | 'remove_secret'
  | 'update_container'
  | 'remove_container'
  | 'dedup_secret'
  | 'enable_management'
  | 'fix_orphaned'

export interface RemediationPlan {
  id: string
  driftIssueId: string
  type: RemediationType
  severity: DriftSeverity
  resource: string
  currentState: string
  proposedState: string
  description: string
  suggestedFix: string
  riskLevel: RiskLevel
  priorityScore: number // 0-100
  affectedContainers: string[]
  affectedSecrets: string[]
  affectedMappings: number
  estimatedImpact: {
    operationalRisk: string
    downtime: string
    dataAtRisk: boolean
    reversible: boolean
  }
  prerequisites: string[]
  postActions: string[]
  estimatedDuration: string
}

export interface RemediationSummary {
  total: number
  critical: number
  high: number
  medium: number
  low: number
  byType: Record<RemediationType, number>
  topOpportunities: RemediationPlan[]
  averagePriorityScore: number
}

/**
 * Create remediation plan from a drift issue
 */
export function createRemediationPlan(
  issue: DriftIssue,
  containers: any[],
  secrets: any[],
  allMappings: Array<{ container: string; secret: string }>
): RemediationPlan {
  const baseId = `remediation-${issue.id}`

  let type: RemediationType = 'add_mapping'
  let suggestedFix = ''
  let proposedState = ''
  let riskLevel: RiskLevel = 'low'
  let affectedContainers: string[] = []
  let affectedSecrets: string[] = []
  let affectedMappings = 0
  let estimatedDuration = '5-15 minutes'

  // Determine remediation type and details based on issue type
  switch (issue.type) {
    case 'discovered_not_managed':
      type = 'enable_management'
      suggestedFix = `Enable DSO management for container "${issue.resource}"`
      proposedState = 'Container will be managed by DSO for configured secrets'
      riskLevel = 'low'
      affectedContainers = [issue.resource]
      estimatedDuration = '10-30 minutes'
      break

    case 'secret_referenced_not_configured':
      type = 'add_secret'
      suggestedFix = `Configure secret "${issue.resource}" in DSO`
      proposedState = 'Secret will be available for container mappings'
      riskLevel = 'high'
      affectedSecrets = [issue.resource]
      estimatedDuration = '15-60 minutes'
      break

    case 'configured_container_missing':
      type = 'remove_container'
      suggestedFix = `Remove references to missing container "${issue.resource}" from configuration`
      proposedState = 'Container references will be cleaned up'
      riskLevel = 'low'
      affectedContainers = [issue.resource]
      estimatedDuration = '5-10 minutes'
      break

    case 'configured_secret_unused':
      type = 'remove_secret'
      suggestedFix = `Remove unused secret "${issue.resource}" from configuration`
      proposedState = 'Secret will no longer be managed'
      riskLevel = 'low'
      affectedSecrets = [issue.resource]
      estimatedDuration = '5-10 minutes'
      break

    case 'sensitive_vars_unmanaged':
      type = 'enable_management'
      suggestedFix = `Configure DSO to manage sensitive variables in "${issue.resource}"`
      proposedState = 'Sensitive variables will be managed by DSO'
      riskLevel = 'high'
      affectedContainers = [issue.resource]
      estimatedDuration = '30-60 minutes'
      break

    case 'partial_management':
      type = 'update_container'
      suggestedFix = `Complete DSO management for container "${issue.resource}"`
      proposedState = 'All secrets will be fully managed by DSO'
      riskLevel = 'medium'
      affectedContainers = [issue.resource]
      estimatedDuration = '20-45 minutes'
      break

    case 'stale_reference':
      type = 'update_container'
      suggestedFix = `Rotate stale secrets for container "${issue.resource}"`
      proposedState = 'All secrets will be rotated and updated'
      riskLevel = 'medium'
      affectedContainers = [issue.resource]
      estimatedDuration = '10-30 minutes'
      break

    case 'missing_mapping':
      type = 'add_mapping'
      suggestedFix = `Create mapping from "${issue.resource}" (inferred)`
      proposedState = 'Mapping will be created based on environment variables'
      riskLevel = 'medium'
      estimatedDuration = '10-20 minutes'
      break

    case 'orphaned_mapping':
      type = 'remove_mapping'
      suggestedFix = `Remove orphaned mapping for "${issue.resource}"`
      proposedState = 'Orphaned mapping will be removed'
      riskLevel = 'low'
      affectedMappings = 1
      estimatedDuration = '5-10 minutes'
      break

    case 'duplicate_mapping':
      type = 'dedup_secret'
      suggestedFix = `Remove duplicate mapping for "${issue.resource}"`
      proposedState = 'Duplicate mapping will be removed'
      riskLevel = 'low'
      affectedMappings = 1
      estimatedDuration = '5-10 minutes'
      break

    case 'duplicate_secret':
      type = 'dedup_secret'
      suggestedFix = `Consolidate duplicate secret "${issue.resource}"`
      proposedState = 'Duplicate secret will be merged'
      riskLevel = 'medium'
      affectedSecrets = [issue.resource]
      estimatedDuration = '20-40 minutes'
      break

    case 'unused_secret':
      type = 'remove_secret'
      suggestedFix = `Archive or remove unused secret "${issue.resource}"`
      proposedState = 'Secret will be archived'
      riskLevel = 'low'
      affectedSecrets = [issue.resource]
      estimatedDuration = '5-15 minutes'
      break

    case 'secret_no_consumers':
      type = 'remove_secret'
      suggestedFix = `Remove secret "${issue.resource}" with no consumers`
      proposedState = 'Secret will be removed from management'
      riskLevel = 'low'
      affectedSecrets = [issue.resource]
      estimatedDuration = '5-10 minutes'
      break

    case 'potential_mapping':
      type = 'add_mapping'
      suggestedFix = `Create mapping for potential secret "${issue.resource}"`
      proposedState = 'Mapping will be created for better security'
      riskLevel = 'low'
      estimatedDuration = '10-20 minutes'
      break
  }

  // Calculate affected mappings count
  if (affectedContainers.length > 0) {
    affectedMappings = allMappings.filter((m) =>
      affectedContainers.includes(m.container)
    ).length
  }
  if (affectedSecrets.length > 0) {
    affectedMappings += allMappings.filter((m) =>
      affectedSecrets.includes(m.secret)
    ).length
  }

  // Estimate impact
  const estimatedImpact = estimateImpact(
    type,
    riskLevel,
    affectedContainers.length,
    affectedSecrets.length,
    affectedMappings
  )

  // Calculate priority score
  const priorityScore = calculatePriorityScore(
    issue.severity,
    affectedContainers.length,
    affectedSecrets.length,
    riskLevel,
    affectedMappings
  )

  return {
    id: baseId,
    driftIssueId: issue.id,
    type,
    severity: issue.severity,
    resource: issue.resource,
    currentState: `Resource "${issue.resource}" - ${issue.type.replace(/_/g, ' ')}`,
    proposedState,
    description: issue.description,
    suggestedFix,
    riskLevel,
    priorityScore,
    affectedContainers,
    affectedSecrets,
    affectedMappings,
    estimatedImpact,
    prerequisites: generatePrerequisites(type, issue.resource),
    postActions: generatePostActions(type, issue.resource),
    estimatedDuration,
  }
}

/**
 * Estimate impact of a remediation
 */
function estimateImpact(
  type: RemediationType,
  riskLevel: RiskLevel,
  containerCount: number,
  secretCount: number,
  mappingCount: number
): RemediationPlan['estimatedImpact'] {
  const dataAtRisk =
    (type === 'remove_secret' || type === 'remove_mapping') &&
    (containerCount > 0 || secretCount > 0)

  const downtime =
    type === 'remove_secret' && containerCount > 0
      ? '5-15 minutes per container'
      : type === 'dedup_secret'
        ? 'Brief (restart required)'
        : 'None expected'

  const operationalRisk =
    riskLevel === 'high'
      ? 'High - may affect running containers'
      : riskLevel === 'medium'
        ? 'Medium - affects specific resources'
        : 'Low - minimal operational impact'

  const reversible =
    type !== 'remove_secret' && type !== 'remove_mapping'

  return {
    operationalRisk,
    downtime,
    dataAtRisk,
    reversible,
  }
}

/**
 * Calculate priority score (0-100)
 * Based on severity, affected resources, and risk level
 */
function calculatePriorityScore(
  severity: DriftSeverity,
  containerCount: number,
  secretCount: number,
  riskLevel: RiskLevel,
  mappingCount: number
): number {
  let score = 0

  // Severity contribution (0-40)
  const severityScores: Record<DriftSeverity, number> = {
    critical: 40,
    warning: 25,
    informational: 10,
  }
  score += severityScores[severity] || 0

  // Affected resources contribution (0-35)
  const resourceScore = Math.min(
    35,
    (containerCount * 5 + secretCount * 7 + mappingCount * 3)
  )
  score += resourceScore

  // Risk level contribution (0-25)
  const riskScores: Record<RiskLevel, number> = {
    high: 25,
    medium: 15,
    low: 5,
  }
  score += riskScores[riskLevel] || 0

  return Math.min(100, Math.round(score))
}

/**
 * Generate prerequisites for remediation
 */
function generatePrerequisites(type: RemediationType, resource: string): string[] {
  const prerequisites: string[] = ['Back up current configuration']

  switch (type) {
    case 'add_secret':
      prerequisites.push(`Ensure secret "${resource}" exists in provider`)
      prerequisites.push('Verify access credentials')
      break
    case 'remove_secret':
      prerequisites.push(`Confirm no active containers depend on "${resource}"`)
      prerequisites.push('Document any dependent services')
      break
    case 'enable_management':
      prerequisites.push(`Verify container "${resource}" is running`)
      prerequisites.push('Ensure adequate permissions')
      break
    case 'dedup_secret':
      prerequisites.push('Identify which copy will be retained')
      prerequisites.push('Update all references to point to retained version')
      break
    case 'remove_mapping':
    case 'remove_container':
      prerequisites.push('Notify affected teams')
      prerequisites.push('Verify no production traffic routed through resource')
      break
  }

  return prerequisites
}

/**
 * Generate post-actions for remediation
 */
function generatePostActions(type: RemediationType, resource: string): string[] {
  const actions: string[] = ['Verify configuration is saved', 'Run validation checks']

  switch (type) {
    case 'add_secret':
    case 'enable_management':
      actions.push(`Test rotation of ${resource}`)
      actions.push('Verify container can access secret')
      actions.push('Monitor logs for any issues')
      break
    case 'remove_secret':
    case 'remove_mapping':
      actions.push('Verify affected containers still running')
      actions.push('Check for any error logs')
      actions.push('Monitor operational metrics')
      break
    case 'dedup_secret':
      actions.push('Verify all references updated')
      actions.push('Remove duplicate entry')
      actions.push('Run full validation')
      break
  }

  actions.push('Document changes in runbook')
  return actions
}

/**
 * Generate remediation plans from drift issues
 */
export function generateRemediationPlans(
  issues: DriftIssue[],
  containers: any[],
  secrets: any[],
  mappings: Array<{ container: string; secret: string }>
): RemediationPlan[] {
  return issues.map((issue) =>
    createRemediationPlan(issue, containers, secrets, mappings)
  )
}

/**
 * Generate remediation summary statistics
 */
export function generateRemediationSummary(
  plans: RemediationPlan[]
): RemediationSummary {
  const summary: RemediationSummary = {
    total: plans.length,
    critical: 0,
    high: 0,
    medium: 0,
    low: 0,
    byType: {} as Record<RemediationType, number>,
    topOpportunities: [],
    averagePriorityScore: 0,
  }

  let totalScore = 0

  plans.forEach((plan) => {
    // Count by risk level
    if (plan.riskLevel === 'high') summary.high++
    else if (plan.riskLevel === 'medium') summary.medium++
    else summary.low++

    // Count by type
    summary.byType[plan.type] = (summary.byType[plan.type] || 0) + 1

    totalScore += plan.priorityScore
  })

  // Count by severity
  summary.critical = plans.filter((p) => p.severity === 'critical').length
  summary.high += plans.filter((p) => p.severity === 'warning' && p.riskLevel === 'high').length
  summary.medium += plans.filter(
    (p) => p.severity === 'informational' && p.riskLevel !== 'high'
  ).length

  // Calculate average priority score
  summary.averagePriorityScore =
    plans.length > 0 ? Math.round(totalScore / plans.length) : 0

  // Get top opportunities (sorted by priority score)
  summary.topOpportunities = [...plans]
    .sort((a, b) => b.priorityScore - a.priorityScore)
    .slice(0, 5)

  return summary
}

/**
 * Filter and search remediation plans
 */
export function filterRemediationPlans(
  plans: RemediationPlan[],
  {
    riskLevel,
    severity,
    searchQuery,
  }: {
    riskLevel?: RiskLevel[]
    severity?: DriftSeverity[]
    searchQuery?: string
  }
): RemediationPlan[] {
  return plans.filter((plan) => {
    // Filter by risk level
    if (riskLevel && !riskLevel.includes(plan.riskLevel)) {
      return false
    }

    // Filter by severity
    if (severity && !severity.includes(plan.severity)) {
      return false
    }

    // Filter by search query
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      return (
        plan.resource.toLowerCase().includes(query) ||
        plan.description.toLowerCase().includes(query) ||
        plan.suggestedFix.toLowerCase().includes(query)
      )
    }

    return true
  })
}

/**
 * Sort remediation plans by priority score
 */
export function sortRemediationPlans(
  plans: RemediationPlan[],
  sortBy: 'priority' | 'resource' | 'type' = 'priority'
): RemediationPlan[] {
  const sorted = [...plans]
  switch (sortBy) {
    case 'priority':
      return sorted.sort((a, b) => b.priorityScore - a.priorityScore)
    case 'resource':
      return sorted.sort((a, b) => a.resource.localeCompare(b.resource))
    case 'type':
      return sorted.sort((a, b) => a.type.localeCompare(b.type))
    default:
      return sorted
  }
}

/**
 * Estimate total operational impact of applying all plans
 */
export function estimateCumulativeImpact(plans: RemediationPlan[]): {
  totalContainers: number
  totalSecrets: number
  totalMappings: number
  containsHighRisk: boolean
  estimatedTimeframe: string
  reversibilityScore: number
} {
  const affectedContainers = new Set<string>()
  const affectedSecrets = new Set<string>()
  let totalMappings = 0
  let reversibleCount = 0

  plans.forEach((plan) => {
    plan.affectedContainers.forEach((c) => affectedContainers.add(c))
    plan.affectedSecrets.forEach((s) => affectedSecrets.add(s))
    totalMappings += plan.affectedMappings
    if (plan.estimatedImpact.reversible) reversibleCount++
  })

  // Estimate timeframe based on number of changes
  const changeCount = affectedContainers.size + affectedSecrets.size
  let timeframe = '0-1 hour'
  if (changeCount > 10) timeframe = '2-4 hours'
  else if (changeCount > 5) timeframe = '1-2 hours'

  return {
    totalContainers: affectedContainers.size,
    totalSecrets: affectedSecrets.size,
    totalMappings,
    containsHighRisk: plans.some((p) => p.riskLevel === 'high'),
    estimatedTimeframe: timeframe,
    reversibilityScore: plans.length > 0 ? Math.round((reversibleCount / plans.length) * 100) : 0,
  }
}
