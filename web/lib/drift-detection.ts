/**
 * Drift Detection Engine
 *
 * Detects configuration mismatches between:
 * - Runtime Discovery (containers)
 * - DSO Configuration (secret mappings)
 * - Secret Definitions (available secrets)
 * - Events (rotation history)
 *
 * All functions are pure and fully memoizable.
 */

export type DriftSeverity = 'critical' | 'warning' | 'informational'
export type DriftCategory = 'container' | 'secret' | 'mapping' | 'configuration'
export type DriftType =
  | 'discovered_not_managed'
  | 'secret_referenced_not_configured'
  | 'configured_container_missing'
  | 'configured_secret_unused'
  | 'sensitive_vars_unmanaged'
  | 'partial_management'
  | 'stale_reference'
  | 'missing_mapping'
  | 'orphaned_mapping'
  | 'duplicate_mapping'
  | 'duplicate_secret'
  | 'unused_secret'
  | 'secret_no_consumers'
  | 'potential_mapping'

export interface DriftIssue {
  id: string
  severity: DriftSeverity
  category: DriftCategory
  type: DriftType
  resource: string
  description: string
  details?: Record<string, unknown>
  recommendedAction: string
  affectedCount?: number
  lastSeen?: string
}

export interface ValidationSummary {
  total: number
  critical: number
  warning: number
  informational: number
  byCategory: Record<DriftCategory, number>
  byType: Record<DriftType, number>
}

/**
 * Detect containers that are discovered but not managed by DSO
 */
export function detectUnmanagedContainers(
  containers: any[],
  managedContainerNames: Set<string>
): DriftIssue[] {
  return containers
    .filter((c) => !managedContainerNames.has(c.name) && c.status === 'running')
    .map((container) => ({
      id: `unmanaged-${container.id}`,
      severity: 'informational' as const,
      category: 'container' as const,
      type: 'discovered_not_managed' as const,
      resource: container.name,
      description: `Container "${container.name}" is running but not managed by DSO`,
      details: {
        image: container.image,
        status: container.status,
        created: container.created,
      },
      recommendedAction: 'Review if this container should be managed by DSO configuration',
    }))
}

/**
 * Detect containers referenced in configuration that no longer exist
 */
export function detectMissingConfiguredContainers(
  configuredContainers: string[],
  actualContainerNames: Set<string>
): DriftIssue[] {
  return configuredContainers
    .filter((name) => !actualContainerNames.has(name))
    .map((containerName) => ({
      id: `missing-container-${containerName}`,
      severity: 'warning' as const,
      category: 'container' as const,
      type: 'configured_container_missing' as const,
      resource: containerName,
      description: `Configured container "${containerName}" no longer exists`,
      details: {
        lastConfigured: new Date().toISOString(),
      },
      recommendedAction: 'Remove this container from DSO configuration or restore the container',
    }))
}

/**
 * Detect containers with sensitive environment variables but no DSO management
 */
export function detectSensitiveVarsWithoutDSO(
  containers: any[],
  managedSecrets: Set<string>
): DriftIssue[] {
  const sensitivePatterns = [
    /password/i,
    /secret/i,
    /token/i,
    /api_?key/i,
    /auth/i,
    /credential/i,
  ]

  return containers
    .filter((c) => {
      const hasSensitiveVars = (c.environment_variable_names || []).some((name: string) =>
        sensitivePatterns.some((pattern) => pattern.test(name))
      )
      const isManagedBySomething = (c.dso_awareness?.managed_secrets || []).some((s: string) =>
        managedSecrets.has(s)
      )
      return hasSensitiveVars && !isManagedBySomething
    })
    .map((container) => ({
      id: `sensitive-unmanaged-${container.id}`,
      severity: 'warning' as const,
      category: 'container' as const,
      type: 'sensitive_vars_unmanaged' as const,
      resource: container.name,
      description: `Container "${container.name}" has sensitive environment variables but is not managed by DSO`,
      details: {
        sensitiveVarCount: (container.environment_variable_names || []).filter((name: string) =>
          sensitivePatterns.some((pattern) => pattern.test(name))
        ).length,
      },
      recommendedAction: 'Consider configuring DSO to manage these sensitive variables as secrets',
    }))
}

/**
 * Detect containers with partial DSO management
 */
export function detectPartiallyManagedContainers(containers: any[]): DriftIssue[] {
  return containers
    .filter((c) => c.dso_awareness?.status === 'partial')
    .map((container) => ({
      id: `partial-${container.id}`,
      severity: 'warning' as const,
      category: 'container' as const,
      type: 'partial_management' as const,
      resource: container.name,
      description: `Container "${container.name}" has partial DSO management`,
      details: {
        managedSecrets: container.dso_awareness?.managed_secrets || [],
        unmanagedSecrets: (container.dso_awareness?.config_refs || []).filter(
          (ref: string) =>
            !(container.dso_awareness?.managed_secrets || []).some((s: string) =>
              ref.includes(s)
            )
        ),
      },
      recommendedAction: 'Review DSO configuration to ensure all secrets are properly managed',
    }))
}

/**
 * Detect secrets configured but not referenced by any container
 */
export function detectUnusedSecrets(
  secrets: any[],
  containerSecretMappings: Map<string, Set<string>>
): DriftIssue[] {
  const usedSecrets = new Set<string>()
  containerSecretMappings.forEach((secrets) => {
    secrets.forEach((s) => usedSecrets.add(s))
  })

  return secrets
    .filter((secret) => !usedSecrets.has(secret.name))
    .map((secret) => ({
      id: `unused-secret-${secret.name}`,
      severity: 'informational' as const,
      category: 'secret' as const,
      type: 'unused_secret' as const,
      resource: secret.name,
      description: `Secret "${secret.name}" is configured but not used by any container`,
      details: {
        provider: secret.provider,
        status: secret.status,
      },
      recommendedAction: 'Remove this secret from configuration if it is no longer needed',
    }))
}

/**
 * Detect secrets referenced in configuration that don't exist
 */
export function detectMissingConfiguredSecrets(
  configuredSecrets: string[],
  actualSecretNames: Set<string>
): DriftIssue[] {
  return configuredSecrets
    .filter((name) => !actualSecretNames.has(name))
    .map((secretName) => ({
      id: `missing-secret-${secretName}`,
      severity: 'critical' as const,
      category: 'secret' as const,
      type: 'secret_referenced_not_configured' as const,
      resource: secretName,
      description: `Secret "${secretName}" is referenced in configuration but not defined`,
      details: {
        lastReferenced: new Date().toISOString(),
      },
      recommendedAction: 'Define this secret or remove references to it from configuration',
    }))
}

/**
 * Detect duplicate secret definitions
 */
export function detectDuplicateSecrets(secrets: any[]): DriftIssue[] {
  const nameCounts = new Map<string, number>()
  secrets.forEach((secret) => {
    nameCounts.set(secret.name, (nameCounts.get(secret.name) || 0) + 1)
  })

  return Array.from(nameCounts.entries())
    .filter(([_, count]) => count > 1)
    .map(([name, count]) => ({
      id: `duplicate-secret-${name}`,
      severity: 'critical' as const,
      category: 'secret' as const,
      type: 'duplicate_secret' as const,
      resource: name,
      description: `Secret "${name}" is defined ${count} times`,
      details: {
        occurrences: count,
      },
      recommendedAction: 'Remove or consolidate duplicate secret definitions',
    }))
}

/**
 * Detect orphaned mappings (mappings to non-existent containers or secrets)
 */
export function detectOrphanedMappings(
  containerNames: Set<string>,
  secretNames: Set<string>,
  mappings: Array<{ container: string; secret: string }>
): DriftIssue[] {
  return mappings
    .filter(
      (mapping) =>
        !containerNames.has(mapping.container) || !secretNames.has(mapping.secret)
    )
    .map((mapping) => ({
      id: `orphaned-mapping-${mapping.container}-${mapping.secret}`,
      severity: 'warning' as const,
      category: 'mapping' as const,
      type: 'orphaned_mapping' as const,
      resource: `${mapping.container} → ${mapping.secret}`,
      description: `Mapping from "${mapping.container}" to "${mapping.secret}" references non-existent resources`,
      details: {
        containerExists: containerNames.has(mapping.container),
        secretExists: secretNames.has(mapping.secret),
      },
      recommendedAction: 'Remove this mapping from configuration',
    }))
}

/**
 * Detect duplicate mappings
 */
export function detectDuplicateMappings(
  mappings: Array<{ container: string; secret: string }>
): DriftIssue[] {
  const seen = new Set<string>()
  const duplicates: Array<{ container: string; secret: string }> = []

  mappings.forEach((mapping) => {
    const key = `${mapping.container}:${mapping.secret}`
    if (seen.has(key)) {
      duplicates.push(mapping)
    }
    seen.add(key)
  })

  return duplicates.map((mapping) => ({
    id: `duplicate-mapping-${mapping.container}-${mapping.secret}`,
    severity: 'warning' as const,
    category: 'mapping' as const,
    type: 'duplicate_mapping' as const,
    resource: `${mapping.container} → ${mapping.secret}`,
    description: `Duplicate mapping from "${mapping.container}" to "${mapping.secret}"`,
    details: {
      container: mapping.container,
      secret: mapping.secret,
    },
    recommendedAction: 'Remove the duplicate mapping',
  }))
}

/**
 * Detect stale references (containers that haven't rotated secrets in a long time)
 */
export function detectStaleReferences(
  containers: any[],
  lastRotationBySecret: Map<string, string>,
  staleThresholdDays: number = 30
): DriftIssue[] {
  const now = new Date()
  const threshold = new Date(now.getTime() - staleThresholdDays * 24 * 60 * 60 * 1000)

  return containers
    .filter((c) => {
      const secrets = c.dso_awareness?.managed_secrets || []
      return secrets.some((secret: string) => {
        const lastRotation = lastRotationBySecret.get(secret)
        return lastRotation && new Date(lastRotation) < threshold
      })
    })
    .map((container) => ({
      id: `stale-${container.id}`,
      severity: 'warning' as const,
      category: 'configuration' as const,
      type: 'stale_reference' as const,
      resource: container.name,
      description: `Container "${container.name}" has secrets that haven't been rotated in ${staleThresholdDays} days`,
      details: {
        staleSecrets: (container.dso_awareness?.managed_secrets || []).filter((secret: string) => {
          const lastRotation = lastRotationBySecret.get(secret)
          return lastRotation && new Date(lastRotation) < threshold
        }),
      },
      recommendedAction: `Trigger a rotation for stale secrets or verify they are still needed`,
    }))
}

/**
 * Main drift detection function - orchestrates all validations
 */
export function detectDriftIssues(
  containers: any[],
  secrets: any[],
  mappings: Array<{ container: string; secret: string }>,
  events: any[] = []
): DriftIssue[] {
  const containerNames = new Set(containers.map((c) => c.name))
  const secretNames = new Set(secrets.map((s) => s.name))
  const managedContainerNames = new Set(
    containers.filter((c) => c.dso_awareness?.status === 'managed').map((c) => c.name)
  )
  const managedSecrets = new Set(secrets.map((s) => s.name))

  // Build container-secret mappings
  const containerSecretMappings = new Map<string, Set<string>>()
  mappings.forEach((mapping) => {
    if (!containerSecretMappings.has(mapping.container)) {
      containerSecretMappings.set(mapping.container, new Set())
    }
    containerSecretMappings.get(mapping.container)!.add(mapping.secret)
  })

  // Build last rotation dates from events
  const lastRotationBySecret = new Map<string, string>()
  events.forEach((event) => {
    if (event.type === 'rotation' && event.secret) {
      const existing = lastRotationBySecret.get(event.secret)
      if (!existing || new Date(event.timestamp) > new Date(existing)) {
        lastRotationBySecret.set(event.secret, event.timestamp)
      }
    }
  })

  // Flatten configured secrets from all containers
  const configuredSecrets: string[] = []
  containerSecretMappings.forEach((secrets) => {
    secrets.forEach((s) => configuredSecrets.push(s))
  })
  const configuredSecretsSet = new Set(configuredSecrets)

  const issues: DriftIssue[] = [
    ...detectUnmanagedContainers(containers, managedContainerNames),
    ...detectMissingConfiguredContainers(
      Array.from(containerSecretMappings.keys()),
      containerNames
    ),
    ...detectSensitiveVarsWithoutDSO(containers, managedSecrets),
    ...detectPartiallyManagedContainers(containers),
    ...detectUnusedSecrets(secrets, containerSecretMappings),
    ...detectMissingConfiguredSecrets(Array.from(configuredSecretsSet), secretNames),
    ...detectDuplicateSecrets(secrets),
    ...detectOrphanedMappings(containerNames, secretNames, mappings),
    ...detectDuplicateMappings(mappings),
    ...detectStaleReferences(containers, lastRotationBySecret),
  ]

  return issues
}

/**
 * Generate validation summary statistics
 */
export function generateValidationSummary(issues: DriftIssue[]): ValidationSummary {
  const summary: ValidationSummary = {
    total: issues.length,
    critical: 0,
    warning: 0,
    informational: 0,
    byCategory: {
      container: 0,
      secret: 0,
      mapping: 0,
      configuration: 0,
    },
    byType: {} as Record<DriftType, number>,
  }

  issues.forEach((issue) => {
    // Count by severity
    if (issue.severity === 'critical') summary.critical++
    else if (issue.severity === 'warning') summary.warning++
    else if (issue.severity === 'informational') summary.informational++

    // Count by category
    summary.byCategory[issue.category]++

    // Count by type
    summary.byType[issue.type] = (summary.byType[issue.type] || 0) + 1
  })

  return summary
}

/**
 * Filter and search drift issues
 */
export function filterDriftIssues(
  issues: DriftIssue[],
  {
    severity,
    category,
    searchQuery,
  }: {
    severity?: DriftSeverity[]
    category?: DriftCategory[]
    searchQuery?: string
  }
): DriftIssue[] {
  return issues.filter((issue) => {
    // Filter by severity
    if (severity && !severity.includes(issue.severity)) {
      return false
    }

    // Filter by category
    if (category && !category.includes(issue.category)) {
      return false
    }

    // Filter by search query
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      return (
        issue.resource.toLowerCase().includes(query) ||
        issue.description.toLowerCase().includes(query) ||
        issue.recommendedAction.toLowerCase().includes(query)
      )
    }

    return true
  })
}

/**
 * Sort drift issues by severity and type
 */
export function sortDriftIssues(
  issues: DriftIssue[],
  sortBy: 'severity' | 'resource' | 'type' = 'severity'
): DriftIssue[] {
  const severityOrder = { critical: 0, warning: 1, informational: 2 }

  const sorted = [...issues]
  switch (sortBy) {
    case 'severity':
      return sorted.sort(
        (a, b) =>
          severityOrder[a.severity] - severityOrder[b.severity] ||
          a.resource.localeCompare(b.resource)
      )
    case 'resource':
      return sorted.sort((a, b) => a.resource.localeCompare(b.resource))
    case 'type':
      return sorted.sort((a, b) => a.type.localeCompare(b.type))
    default:
      return sorted
  }
}
