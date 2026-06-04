/**
 * Change Set Engine
 *
 * Converts remediation plans into explicit change sets with:
 * - Current vs proposed state
 * - Diff generation
 * - Validation rules
 * - Impact tracking
 * - Approval simulation
 *
 * All functions are pure and fully memoizable.
 */

import { RemediationPlan, RiskLevel } from './remediation-planner'

export type ApprovalStatus = 'pending' | 'approved' | 'rejected'

export interface StateValue {
  type: string
  name: string
  properties?: Record<string, unknown>
  relationships?: string[]
}

export interface DiffLine {
  type: 'add' | 'remove' | 'modify' | 'context'
  current?: string
  proposed?: string
  description?: string
}

export interface ValidationRule {
  id: string
  type: 'precondition' | 'dependency' | 'conflict' | 'missing_resource' | 'warning'
  severity: 'error' | 'warning' | 'info'
  message: string
  details?: string
}

export interface ImpactNode {
  id: string
  label: string
  type: 'secret' | 'container' | 'mapping' | 'event'
  affectedCount: number
}

export interface ImpactEdge {
  from: string
  to: string
  label: string
  type: 'uses' | 'triggers' | 'references'
}

export interface ChangeSet {
  id: string
  remediationPlanId: string
  title: string
  description: string
  priority: number // 0-100
  riskLevel: RiskLevel
  approvalStatus: ApprovalStatus
  createdAt: string
  estimatedDuration: string

  // State information
  currentState: StateValue
  proposedState: StateValue

  // Diff information
  diff: DiffLine[]
  diffSummary: {
    additions: number
    removals: number
    modifications: number
  }

  // Validation
  validationRules: ValidationRule[]
  isValid: boolean
  blockers: ValidationRule[]
  warnings: ValidationRule[]

  // Impact
  affectedResources: {
    containers: string[]
    secrets: string[]
    mappings: number
    events: number
  }
  impactGraph: {
    nodes: ImpactNode[]
    edges: ImpactEdge[]
  }

  // Metadata
  executionOrder: number
  estimatedImpact: string
}

export interface ChangeSetSummary {
  total: number
  pending: number
  approved: number
  rejected: number
  critical: number
  high: number
  medium: number
  low: number
  blockerCount: number
  totalAdditions: number
  totalRemovals: number
  totalModifications: number
}

/**
 * Generate current state from containers, secrets, mappings
 */
function generateCurrentState(
  resourceName: string,
  containers: any[],
  secrets: any[],
  mappings: Array<{ container: string; secret: string }>
): StateValue {
  // Find the resource
  const container = containers.find((c) => c.name === resourceName)
  const secret = secrets.find((s) => s.name === resourceName)
  const isMapping = mappings.some(
    (m) =>
      (m.container === resourceName || m.secret === resourceName) &&
      mappings.filter((x) => x === m).length > 0
  )

  if (container) {
    return {
      type: 'container',
      name: container.name,
      properties: {
        status: container.status,
        image: container.image,
        dsoStatus: container.dso_awareness?.status,
        managedSecrets: container.dso_awareness?.managed_secrets || [],
      },
      relationships: mappings
        .filter((m) => m.container === container.name)
        .map((m) => m.secret),
    }
  }

  if (secret) {
    return {
      type: 'secret',
      name: secret.name,
      properties: {
        provider: secret.provider,
        status: secret.status,
        lastRotated: secret.last_rotated,
      },
      relationships: mappings
        .filter((m) => m.secret === secret.name)
        .map((m) => m.container),
    }
  }

  return {
    type: 'resource',
    name: resourceName,
    properties: {},
    relationships: [],
  }
}

/**
 * Generate proposed state based on remediation type
 */
function generateProposedState(
  plan: RemediationPlan,
  currentState: StateValue
): StateValue {
  const proposed = JSON.parse(JSON.stringify(currentState))

  switch (plan.type) {
    case 'enable_management':
      proposed.properties = {
        ...proposed.properties,
        dsoStatus: 'managed',
        managedSecrets: proposed.relationships || [],
      }
      break

    case 'add_mapping':
      proposed.properties = {
        ...proposed.properties,
        newMapping: true,
      }
      proposed.relationships = [
        ...(proposed.relationships || []),
        plan.resource,
      ]
      break

    case 'remove_mapping':
      proposed.relationships = (proposed.relationships || []).filter(
        (r: string) => r !== plan.resource
      )
      break

    case 'add_secret':
      proposed.properties = {
        ...proposed.properties,
        status: 'configured',
      }
      break

    case 'remove_secret':
      proposed.properties = {
        ...proposed.properties,
        status: 'archived',
      }
      break

    case 'dedup_secret':
      proposed.properties = {
        ...proposed.properties,
        consolidated: true,
      }
      break
  }

  return proposed
}

/**
 * Generate diff between current and proposed state
 */
function generateDiff(
  currentState: StateValue,
  proposedState: StateValue
): DiffLine[] {
  const diff: DiffLine[] = []

  // Add header
  diff.push({
    type: 'context',
    description: `Proposed changes to ${currentState.name}`,
  })

  // Compare properties
  const currentProps = currentState.properties || {}
  const proposedProps = proposedState.properties || {}
  const allKeys = new Set([...Object.keys(currentProps), ...Object.keys(proposedProps)])

  allKeys.forEach((key) => {
    const currentVal = JSON.stringify(currentProps[key])
    const proposedVal = JSON.stringify(proposedProps[key])

    if (currentVal !== proposedVal) {
      if (!(key in currentProps)) {
        diff.push({
          type: 'add',
          proposed: `+ ${key}: ${proposedVal}`,
          description: `Add ${key}`,
        })
      } else if (!(key in proposedProps)) {
        diff.push({
          type: 'remove',
          current: `- ${key}: ${currentVal}`,
          description: `Remove ${key}`,
        })
      } else {
        diff.push({
          type: 'modify',
          current: `- ${key}: ${currentVal}`,
          proposed: `+ ${key}: ${proposedVal}`,
          description: `Update ${key}`,
        })
      }
    }
  })

  // Compare relationships
  const currentRels = new Set(currentState.relationships || [])
  const proposedRels = new Set(proposedState.relationships || [])

  currentRels.forEach((rel) => {
    if (!proposedRels.has(rel)) {
      diff.push({
        type: 'remove',
        current: `- relationship: ${rel}`,
        description: `Remove relationship to ${rel}`,
      })
    }
  })

  proposedRels.forEach((rel) => {
    if (!currentRels.has(rel)) {
      diff.push({
        type: 'add',
        proposed: `+ relationship: ${rel}`,
        description: `Add relationship to ${rel}`,
      })
    }
  })

  return diff
}

/**
 * Generate validation rules for a change set
 */
function generateValidationRules(
  plan: RemediationPlan,
  currentState: StateValue,
  containers: any[],
  secrets: any[],
  mappings: Array<{ container: string; secret: string }>
): ValidationRule[] {
  const rules: ValidationRule[] = []

  // Preconditions
  if (plan.type === 'add_secret') {
    const secretExists = secrets.some((s) => s.name === plan.resource)
    rules.push({
      id: 'secret-exists',
      type: 'precondition',
      severity: secretExists ? 'info' : 'error',
      message: secretExists ? '✓ Secret exists in provider' : '✗ Secret does not exist in provider',
      details: secretExists ? undefined : `Secret "${plan.resource}" must be configured before mapping`,
    })
  }

  if (plan.type === 'enable_management') {
    const containerRunning =
      containers.find((c) => c.name === plan.resource)?.status === 'running'
    rules.push({
      id: 'container-running',
      type: 'precondition',
      severity: containerRunning ? 'info' : 'warning',
      message: containerRunning ? '✓ Container is running' : '⚠ Container is not running',
      details: containerRunning ? undefined : 'Container should be running before enabling management',
    })
  }

  // Dependencies
  const affectedMappings = mappings.filter(
    (m) => m.container === plan.resource || m.secret === plan.resource
  )
  if (affectedMappings.length > 0) {
    rules.push({
      id: 'dependent-mappings',
      type: 'dependency',
      severity: 'info',
      message: `${affectedMappings.length} dependent mapping(s) affected`,
      details: `This change affects: ${affectedMappings.map((m) => `${m.container}→${m.secret}`).join(', ')}`,
    })
  }

  // Conflicts
  if (plan.type === 'dedup_secret') {
    const duplicates = secrets.filter((s) => s.name === plan.resource).length
    if (duplicates > 1) {
      rules.push({
        id: 'duplicate-conflict',
        type: 'conflict',
        severity: 'warning',
        message: `${duplicates} duplicate definitions found`,
        details: 'All duplicates must be reviewed before consolidation',
      })
    }
  }

  // Missing resources
  if (plan.type === 'add_mapping') {
    const containerExists = containers.some((c) => c.name === plan.resource)
    const secretExists = secrets.some((s) => s.name === plan.resource)

    if (!containerExists && !secretExists) {
      rules.push({
        id: 'missing-resource',
        type: 'missing_resource',
        severity: 'error',
        message: `Resource "${plan.resource}" does not exist`,
        details: 'Must create resource before mapping',
      })
    }
  }

  // Warnings
  if (plan.riskLevel === 'high') {
    rules.push({
      id: 'high-risk-warning',
      type: 'warning',
      severity: 'warning',
      message: '⚠ High-risk change: Operator approval recommended',
      details: 'Review impact carefully before applying this change',
    })
  }

  if (plan.affectedContainers.length > 5) {
    rules.push({
      id: 'multiple-containers',
      type: 'warning',
      severity: 'warning',
      message: `This change affects ${plan.affectedContainers.length} containers`,
      details: 'Consider applying in batches to minimize impact',
    })
  }

  return rules
}

/**
 * Generate impact graph for visualization
 */
function generateImpactGraph(
  plan: RemediationPlan,
  currentState: StateValue
): { nodes: ImpactNode[]; edges: ImpactEdge[] } {
  const nodes: ImpactNode[] = []
  const edges: ImpactEdge[] = []

  // Add resource nodes
  if (currentState.type === 'secret') {
    nodes.push({
      id: `secret-${currentState.name}`,
      label: currentState.name,
      type: 'secret',
      affectedCount: plan.affectedContainers.length,
    })

    // Add container nodes
    plan.affectedContainers.forEach((container) => {
      const nodeId = `container-${container}`
      if (!nodes.find((n) => n.id === nodeId)) {
        nodes.push({
          id: nodeId,
          label: container,
          type: 'container',
          affectedCount: 1,
        })
      }

      edges.push({
        from: `secret-${currentState.name}`,
        to: nodeId,
        label: 'manages',
        type: 'uses',
      })
    })
  } else if (currentState.type === 'container') {
    nodes.push({
      id: `container-${currentState.name}`,
      label: currentState.name,
      type: 'container',
      affectedCount: 1,
    })

    // Add secret nodes
    const relationships = currentState.relationships || []
    relationships.forEach((secret) => {
      const nodeId = `secret-${secret}`
      if (!nodes.find((n) => n.id === nodeId)) {
        nodes.push({
          id: nodeId,
          label: secret,
          type: 'secret',
          affectedCount: 1,
        })
      }

      edges.push({
        from: nodeId,
        to: `container-${currentState.name}`,
        label: 'manages',
        type: 'uses',
      })
    })
  }

  return { nodes, edges }
}

/**
 * Create a change set from a remediation plan
 */
export function createChangeSet(
  plan: RemediationPlan,
  containers: any[],
  secrets: any[],
  mappings: Array<{ container: string; secret: string }>,
  executionOrder: number
): ChangeSet {
  const currentState = generateCurrentState(plan.resource, containers, secrets, mappings)
  const proposedState = generateProposedState(plan, currentState)
  const diff = generateDiff(currentState, proposedState)
  const validationRules = generateValidationRules(plan, currentState, containers, secrets, mappings)
  const impactGraph = generateImpactGraph(plan, currentState)

  const diffSummary = {
    additions: diff.filter((d) => d.type === 'add').length,
    removals: diff.filter((d) => d.type === 'remove').length,
    modifications: diff.filter((d) => d.type === 'modify').length,
  }

  const blockers = validationRules.filter((r) => r.severity === 'error')
  const warnings = validationRules.filter((r) => r.severity === 'warning')

  return {
    id: `changeset-${plan.id}`,
    remediationPlanId: plan.id,
    title: plan.suggestedFix,
    description: plan.description,
    priority: plan.priorityScore,
    riskLevel: plan.riskLevel,
    approvalStatus: 'pending',
    createdAt: new Date().toISOString(),
    estimatedDuration: plan.estimatedDuration,

    currentState,
    proposedState,

    diff: diff.slice(1), // Remove header
    diffSummary,

    validationRules,
    isValid: blockers.length === 0,
    blockers,
    warnings,

    affectedResources: {
      containers: plan.affectedContainers,
      secrets: plan.affectedSecrets,
      mappings: plan.affectedMappings,
      events: 0, // Would be populated from event analysis
    },
    impactGraph,

    executionOrder,
    estimatedImpact: `${plan.affectedContainers.length} containers, ${plan.affectedSecrets.length} secrets`,
  }
}

/**
 * Generate all change sets from remediation plans
 */
export function generateChangeSets(
  plans: RemediationPlan[],
  containers: any[],
  secrets: any[],
  mappings: Array<{ container: string; secret: string }>
): ChangeSet[] {
  return plans.map((plan, index) =>
    createChangeSet(plan, containers, secrets, mappings, index + 1)
  )
}

/**
 * Generate change set summary statistics
 */
export function generateChangeSetSummary(changeSets: ChangeSet[]): ChangeSetSummary {
  const summary: ChangeSetSummary = {
    total: changeSets.length,
    pending: 0,
    approved: 0,
    rejected: 0,
    critical: 0,
    high: 0,
    medium: 0,
    low: 0,
    blockerCount: 0,
    totalAdditions: 0,
    totalRemovals: 0,
    totalModifications: 0,
  }

  changeSets.forEach((cs) => {
    // Count by approval status
    if (cs.approvalStatus === 'pending') summary.pending++
    else if (cs.approvalStatus === 'approved') summary.approved++
    else if (cs.approvalStatus === 'rejected') summary.rejected++

    // Count by risk
    if (cs.riskLevel === 'high') summary.high++
    else if (cs.riskLevel === 'medium') summary.medium++
    else summary.low++

    // Count blockers
    summary.blockerCount += cs.blockers.length

    // Count diffs
    summary.totalAdditions += cs.diffSummary.additions
    summary.totalRemovals += cs.diffSummary.removals
    summary.totalModifications += cs.diffSummary.modifications
  })

  // Mark critical if blockers exist
  summary.critical = summary.blockerCount > 0 ? 1 : 0

  return summary
}

/**
 * Filter change sets by status or risk
 */
export function filterChangeSets(
  changeSets: ChangeSet[],
  { approvalStatus, riskLevel }: { approvalStatus?: ApprovalStatus[]; riskLevel?: RiskLevel[] }
): ChangeSet[] {
  return changeSets.filter((cs) => {
    if (approvalStatus && !approvalStatus.includes(cs.approvalStatus)) return false
    if (riskLevel && !riskLevel.includes(cs.riskLevel)) return false
    return true
  })
}

/**
 * Sort change sets by execution order or priority
 */
export function sortChangeSets(
  changeSets: ChangeSet[],
  sortBy: 'order' | 'priority' | 'risk' = 'order'
): ChangeSet[] {
  const sorted = [...changeSets]
  switch (sortBy) {
    case 'order':
      return sorted.sort((a, b) => a.executionOrder - b.executionOrder)
    case 'priority':
      return sorted.sort((a, b) => b.priority - a.priority)
    case 'risk':
      const riskOrder: Record<RiskLevel, number> = { high: 0, medium: 1, low: 2 }
      return sorted.sort(
        (a, b) => riskOrder[a.riskLevel] - riskOrder[b.riskLevel]
      )
    default:
      return sorted
  }
}

/**
 * Simulate approval status change (no persistence)
 */
export function simulateApproval(
  changeSet: ChangeSet,
  status: ApprovalStatus
): ChangeSet {
  return {
    ...changeSet,
    approvalStatus: status,
  }
}

/**
 * Generate impact graph visualization as ASCII
 */
export function visualizeImpactGraph(graph: { nodes: ImpactNode[]; edges: ImpactEdge[] }): string {
  let ascii = 'Impact Relationship Graph:\n\n'

  // Group by type
  const secrets = graph.nodes.filter((n) => n.type === 'secret')
  const containers = graph.nodes.filter((n) => n.type === 'container')

  ascii += 'Secrets:\n'
  secrets.forEach((s) => {
    ascii += `  [S] ${s.label} (affects ${s.affectedCount})\n`
  })

  ascii += '\nContainers:\n'
  containers.forEach((c) => {
    ascii += `  [C] ${c.label}\n`
  })

  ascii += '\nRelationships:\n'
  graph.edges.forEach((e) => {
    const fromNode = graph.nodes.find((n) => n.id === e.from)
    const toNode = graph.nodes.find((n) => n.id === e.to)
    if (fromNode && toNode) {
      ascii += `  ${fromNode.label} --${e.label}--> ${toNode.label}\n`
    }
  })

  return ascii
}
