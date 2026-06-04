/**
 * Workspace Validation Integration
 *
 * Validates draft configuration against:
 * - Drift Detection rules
 * - Remediation constraints
 * - Change Set validation rules
 * - Conflict detection
 *
 * All validation is live and non-destructive.
 */

import { ValidationRule } from './change-set'
import type { WorkspaceState } from './workspace'

export type ValidationSeverity = 'error' | 'warning' | 'info'

export interface WorkspaceValidationResult {
  id: string
  severity: ValidationSeverity
  category: 'conflict' | 'dependency' | 'orphaned' | 'duplicate' | 'missing' | 'warning'
  message: string
  details?: string
  affectedResources?: string[]
  suggestedFix?: string
}

export interface WorkspaceValidationSummary {
  total: number
  errors: number
  warnings: number
  infos: number
  hasCriticalIssues: boolean
  byCategory: Record<string, number>
}

/**
 * Validate draft configuration for conflicts
 */
export function validateDraftConfiguration(
  workspace: WorkspaceState,
  currentContainers: any[],
  currentSecrets: any[],
  currentMappings: Array<{ container: string; secret: string }>
): WorkspaceValidationResult[] {
  const results: WorkspaceValidationResult[] = []

  // Check for duplicate mappings
  const mappingKeys = new Set<string>()
  workspace.config.mappings.forEach((mapping) => {
    const key = `${mapping.container}:${mapping.secret}`
    if (mappingKeys.has(key)) {
      results.push({
        id: `dup-mapping-${key}`,
        severity: 'error',
        category: 'duplicate',
        message: `Duplicate mapping: ${mapping.container} → ${mapping.secret}`,
        details: 'This mapping is defined multiple times in the draft',
        affectedResources: [mapping.container, mapping.secret],
        suggestedFix: 'Remove one of the duplicate mappings',
      })
    }
    mappingKeys.add(key)
  })

  // Check for orphaned mappings (container doesn't exist)
  const containerNames = new Set(currentContainers.map((c: any) => c.name))
  workspace.config.mappings.forEach((mapping) => {
    if (!containerNames.has(mapping.container)) {
      results.push({
        id: `orphaned-container-${mapping.container}`,
        severity: 'warning',
        category: 'orphaned',
        message: `Container "${mapping.container}" does not exist`,
        details: 'The container referenced in this mapping was not found',
        affectedResources: [mapping.container],
        suggestedFix: 'Remove this mapping or create the container',
      })
    }
  })

  // Check for orphaned mappings (secret doesn't exist)
  const secretNames = new Set(currentSecrets.map((s: any) => s.name))
  workspace.config.mappings.forEach((mapping) => {
    if (!secretNames.has(mapping.secret)) {
      results.push({
        id: `orphaned-secret-${mapping.secret}`,
        severity: 'error',
        category: 'orphaned',
        message: `Secret "${mapping.secret}" does not exist`,
        details: 'The secret referenced in this mapping was not found',
        affectedResources: [mapping.secret],
        suggestedFix: 'Add the secret or remove this mapping',
      })
    }
  })

  // Check for missing secret definitions
  const draftSecretNames = new Set(workspace.config.secrets.map((s) => s.name))
  workspace.config.mappings.forEach((mapping) => {
    if (!draftSecretNames.has(mapping.secret)) {
      results.push({
        id: `missing-secret-def-${mapping.secret}`,
        severity: 'warning',
        category: 'missing',
        message: `Secret "${mapping.secret}" is mapped but not defined`,
        details: 'Add a secret definition for this mapping',
        affectedResources: [mapping.secret],
        suggestedFix: `Add secret definition for "${mapping.secret}"`,
      })
    }
  })

  // Check for duplicate secret definitions
  const secretCounts = new Map<string, number>()
  workspace.config.secrets.forEach((secret) => {
    secretCounts.set(secret.name, (secretCounts.get(secret.name) || 0) + 1)
  })
  secretCounts.forEach((count, name) => {
    if (count > 1) {
      results.push({
        id: `dup-secret-${name}`,
        severity: 'error',
        category: 'duplicate',
        message: `Secret "${name}" is defined ${count} times`,
        details: 'Consolidate these definitions into a single entry',
        affectedResources: [name],
        suggestedFix: 'Remove duplicate secret definitions',
      })
    }
  })

  // Check for provider conflicts (same secret with different providers)
  const secretProviders = new Map<string, Set<string>>()
  workspace.config.secrets.forEach((secret) => {
    if (!secretProviders.has(secret.name)) {
      secretProviders.set(secret.name, new Set())
    }
    secretProviders.get(secret.name)!.add(secret.provider)
  })
  secretProviders.forEach((providers, name) => {
    if (providers.size > 1) {
      results.push({
        id: `provider-conflict-${name}`,
        severity: 'error',
        category: 'conflict',
        message: `Secret "${name}" has conflicting providers: ${Array.from(providers).join(', ')}`,
        details: 'A secret cannot be defined with multiple providers',
        affectedResources: [name],
        suggestedFix: 'Choose one provider for this secret',
      })
    }
  })

  // Check for large-scale changes
  const addedMappings = workspace.config.mappings.filter(
    (m) => !currentMappings.some((cm) => cm.container === m.container && cm.secret === m.secret)
  ).length

  if (addedMappings > 10) {
    results.push({
      id: 'large-change-warning',
      severity: 'warning',
      category: 'warning',
      message: `Large change: ${addedMappings} mappings being added`,
      details: 'Consider reviewing and applying changes in smaller batches',
      suggestedFix: 'Review the draft carefully before applying',
    })
  }

  return results
}

/**
 * Generate validation summary
 */
export function generateValidationSummary(
  results: WorkspaceValidationResult[]
): WorkspaceValidationSummary {
  const summary: WorkspaceValidationSummary = {
    total: results.length,
    errors: 0,
    warnings: 0,
    infos: 0,
    hasCriticalIssues: false,
    byCategory: {},
  }

  results.forEach((result) => {
    if (result.severity === 'error') {
      summary.errors++
      summary.hasCriticalIssues = true
    } else if (result.severity === 'warning') {
      summary.warnings++
    } else {
      summary.infos++
    }

    summary.byCategory[result.category] = (summary.byCategory[result.category] || 0) + 1
  })

  return summary
}

/**
 * Create from drift issue
 */
export function createDraftFromDriftIssue(
  issue: any,
  containers: any[],
  secrets: any[]
): Array<{ type: string; container?: string; secret?: string }> {
  const changes: Array<{ type: string; container?: string; secret?: string }> = []

  switch (issue.type) {
    case 'secret_referenced_not_configured':
      changes.push({
        type: 'add_secret',
        secret: issue.resource,
      })
      break

    case 'sensitive_vars_unmanaged':
      const container = containers.find((c) => c.name === issue.resource)
      if (container) {
        const sensitiveSectionMatch = container.environment_variable_names?.filter(
          (name: string) => /password|secret|token|key|auth/i.test(name)
        )
        if (sensitiveSectionMatch && sensitiveSectionMatch.length > 0) {
          changes.push({
            type: 'add_mapping',
            container: container.name,
            secret: `${container.name}-secrets`,
          })
        }
      }
      break

    case 'orphaned_mapping':
      const parts = issue.resource.split('→')
      if (parts.length === 2) {
        changes.push({
          type: 'remove_mapping',
          container: parts[0].trim(),
          secret: parts[1].trim(),
        })
      }
      break
  }

  return changes
}

/**
 * Create from remediation plan
 */
export function createDraftFromRemediationPlan(
  plan: any
): Array<{ type: string; container?: string; secret?: string }> {
  const changes: Array<{ type: string; container?: string; secret?: string }> = []

  switch (plan.type) {
    case 'add_mapping':
      const mapParts = plan.resource.split('→')
      if (mapParts.length === 2) {
        changes.push({
          type: 'add_mapping',
          container: mapParts[0].trim(),
          secret: mapParts[1].trim(),
        })
      }
      break

    case 'remove_mapping':
      const removeParts = plan.resource.split('→')
      if (removeParts.length === 2) {
        changes.push({
          type: 'remove_mapping',
          container: removeParts[0].trim(),
          secret: removeParts[1].trim(),
        })
      }
      break

    case 'add_secret':
      changes.push({
        type: 'add_secret',
        secret: plan.resource,
      })
      break

    case 'enable_management':
      plan.affectedSecrets?.forEach((secret: string) => {
        changes.push({
          type: 'add_mapping',
          container: plan.resource,
          secret,
        })
      })
      break
  }

  return changes
}

/**
 * Create from change set
 */
export function createDraftFromChangeSet(changeSet: any): Array<{ type: string; container?: string; secret?: string }> {
  const changes: Array<{ type: string; container?: string; secret?: string }> = []

  changeSet.diff?.forEach((diff: any) => {
    if (diff.type === 'add' && diff.description?.includes('mapping')) {
      const match = diff.proposed?.match(/relationship: (.+?)→(.+)/)
      if (match) {
        changes.push({
          type: 'add_mapping',
          container: match[1].trim(),
          secret: match[2].trim(),
        })
      }
    }
  })

  return changes
}

/**
 * Filter validation results
 */
export function filterValidationResults(
  results: WorkspaceValidationResult[],
  severities: ValidationSeverity[]
): WorkspaceValidationResult[] {
  return results.filter((r) => severities.includes(r.severity))
}

/**
 * Sort validation results by severity
 */
export function sortValidationResults(results: WorkspaceValidationResult[]): WorkspaceValidationResult[] {
  const severityOrder: Record<ValidationSeverity, number> = { error: 0, warning: 1, info: 2 }
  return [...results].sort((a, b) => severityOrder[a.severity] - severityOrder[b.severity])
}
