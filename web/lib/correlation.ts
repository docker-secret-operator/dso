/**
 * Correlation Engine
 * Pure functions for correlating Secrets, Containers, and Events
 * No side effects, memoizable, performance-optimized
 */

export interface ContainerRelation {
  id: string
  name: string
  image: string
  status: string
  dsoStatus: string
}

export interface SecretRelation {
  name: string
  provider: string
  status: string
  lastRotated?: string
}

export interface EventRelation {
  id: string
  timestamp: string
  severity: 'info' | 'warning' | 'error'
  title: string
  message: string
}

export interface CorrelatedResource {
  type: 'secret' | 'container' | 'event'
  name: string
  severity?: 'info' | 'warning' | 'error'
  count?: number
}

/**
 * Get all secrets used by a specific container
 * Pure function - can be memoized
 */
export function getSecretsForContainer(
  containerName: string,
  containers: any[],
  secrets: any[]
): SecretRelation[] {
  const container = containers.find((c) => c.name === containerName)
  if (!container) return []

  // Find secrets that match the container's managed/configured secrets
  const containerSecretNames = container.dso_awareness?.managed_secrets || []
  return secrets.filter((s: any) => containerSecretNames.includes(s.name)).map((s: any) => ({
    name: s.name,
    provider: s.provider || 'Unknown',
    status: s.status || 'healthy',
    lastRotated: s.last_rotated,
  }))
}

/**
 * Get all containers using a specific secret
 * Pure function - can be memoized
 */
export function getContainersForSecret(secretName: string, containers: any[]): ContainerRelation[] {
  return containers
    .filter((c: any) => c.dso_awareness?.managed_secrets?.includes(secretName))
    .map((c: any) => ({
      id: c.id,
      name: c.name,
      image: c.image,
      status: c.status,
      dsoStatus: c.dso_awareness?.status || 'unmanaged',
    }))
}

/**
 * Get all events related to a specific secret
 * Pure function - can be memoized
 */
export function getEventsForSecret(secretName: string, events: any[]): EventRelation[] {
  return events
    .filter(
      (e: any) =>
        e.message?.toLowerCase().includes(secretName.toLowerCase()) ||
        e.secret?.toLowerCase().includes(secretName.toLowerCase())
    )
    .map((e: any) => ({
      id: e.id || `event-${e.timestamp}`,
      timestamp: e.timestamp || new Date().toISOString(),
      severity: mapEventSeverity(e.level),
      title: e.message || 'Event',
      message: e.message || '',
    }))
}

/**
 * Get all events related to a specific container
 * Pure function - can be memoized
 */
export function getEventsForContainer(containerName: string, events: any[]): EventRelation[] {
  return events
    .filter(
      (e: any) =>
        e.message?.toLowerCase().includes(containerName.toLowerCase()) ||
        e.container?.toLowerCase().includes(containerName.toLowerCase())
    )
    .map((e: any) => ({
      id: e.id || `event-${e.timestamp}`,
      timestamp: e.timestamp || new Date().toISOString(),
      severity: mapEventSeverity(e.level),
      title: e.message || 'Event',
      message: e.message || '',
    }))
}

/**
 * Get related resources (secrets and containers) connected to a specific resource
 * Pure function - can be memoized
 */
export function getRelatedResources(
  resourceName: string,
  resourceType: 'secret' | 'container',
  containers: any[],
  secrets: any[]
): CorrelatedResource[] {
  const resources: CorrelatedResource[] = []

  if (resourceType === 'secret') {
    // Find containers using this secret
    const affectedContainers = getContainersForSecret(resourceName, containers)
    resources.push(
      ...affectedContainers.map((c) => ({
        type: 'container' as const,
        name: c.name,
        count: 1,
      }))
    )
  } else if (resourceType === 'container') {
    // Find secrets used by this container
    const relatedSecrets = getSecretsForContainer(resourceName, containers, secrets)
    resources.push(
      ...relatedSecrets.map((s) => ({
        type: 'secret' as const,
        name: s.name,
        severity: (s.status === 'error' ? 'error' : s.status === 'warning' ? 'warning' : 'info') as
          | 'info'
          | 'warning'
          | 'error',
        count: 1,
      }))
    )
  }

  return resources
}

/**
 * Calculate impact metrics for a secret
 * Used to determine severity of secret issues
 * Pure function - can be memoized
 */
export function calculateSecretImpact(
  secretName: string,
  containers: any[],
  events: any[]
): {
  affectedContainers: number
  recentErrors: number
  severity: 'high' | 'medium' | 'low'
} {
  const affectedContainers = getContainersForSecret(secretName, containers).length
  const recentErrors = getEventsForSecret(secretName, events).filter((e) => e.severity === 'error')
    .length

  let severity: 'high' | 'medium' | 'low' = 'low'
  if (recentErrors > 0 || affectedContainers > 10) severity = 'high'
  else if (recentErrors === 0 && affectedContainers > 5) severity = 'medium'

  return { affectedContainers, recentErrors, severity }
}

/**
 * Calculate impact metrics for a container
 * Pure function - can be memoized
 */
export function calculateContainerImpact(
  containerName: string,
  secrets: any[],
  events: any[]
): {
  managedSecrets: number
  recentErrors: number
  severity: 'high' | 'medium' | 'low'
} {
  const container = (containers: any[]) =>
    containers.find((c: any) => c.name === containerName)?.dso_awareness?.managed_secrets || []

  const managedSecrets = secrets.filter((s: any) => container([]).includes(s.name)).length
  const recentErrors = getEventsForContainer(containerName, events).filter((e) => e.severity === 'error')
    .length

  let severity: 'high' | 'medium' | 'low' = 'low'
  if (recentErrors > 0) severity = 'high'
  else if (managedSecrets === 0) severity = 'medium'

  return { managedSecrets, recentErrors, severity }
}

/**
 * Find operational hotspots - resources with high impact issues
 * Pure function - can be memoized
 */
export interface OperationalHotspot {
  resource: string
  type: 'secret' | 'container'
  affectedCount: number
  recentIssues: number
  severity: 'high' | 'medium' | 'low'
}

export function findOperationalHotspots(
  containers: any[],
  secrets: any[],
  events: any[],
  limit = 10
): OperationalHotspot[] {
  const hotspots: OperationalHotspot[] = []

  // Find secret hotspots
  secrets.forEach((secret: any) => {
    const impact = calculateSecretImpact(secret.name, containers, events)
    if (impact.severity !== 'low') {
      hotspots.push({
        resource: secret.name,
        type: 'secret',
        affectedCount: impact.affectedContainers,
        recentIssues: impact.recentErrors,
        severity: impact.severity,
      })
    }
  })

  // Sort by severity and recent issues
  hotspots.sort((a, b) => {
    const severityOrder = { high: 0, medium: 1, low: 2 }
    const severityDiff = severityOrder[a.severity] - severityOrder[b.severity]
    if (severityDiff !== 0) return severityDiff
    return b.recentIssues - a.recentIssues
  })

  return hotspots.slice(0, limit)
}

/**
 * Helper: Map event level to severity
 */
function mapEventSeverity(level: string): 'info' | 'warning' | 'error' {
  const levelLower = (level || 'info').toLowerCase()
  if (levelLower.includes('error')) return 'error'
  if (levelLower.includes('warn')) return 'warning'
  return 'info'
}

/**
 * Get correlation summary between resource and related items
 * Pure function - can be memoized
 */
export function getCorrelationSummary(
  resourceName: string,
  resourceType: 'secret' | 'container',
  containers: any[],
  secrets: any[],
  events: any[]
) {
  if (resourceType === 'secret') {
    const relatedContainers = getContainersForSecret(resourceName, containers)
    const relatedEvents = getEventsForSecret(resourceName, events)

    return {
      resource: resourceName,
      type: 'secret' as const,
      relatedContainers,
      relatedEvents,
      totalContainers: relatedContainers.length,
      totalEvents: relatedEvents.length,
      errorCount: relatedEvents.filter((e) => e.severity === 'error').length,
      warningCount: relatedEvents.filter((e) => e.severity === 'warning').length,
    }
  } else {
    const relatedSecrets = getSecretsForContainer(resourceName, containers, secrets)
    const relatedEvents = getEventsForContainer(resourceName, events)

    return {
      resource: resourceName,
      type: 'container' as const,
      relatedSecrets,
      relatedEvents,
      totalSecrets: relatedSecrets.length,
      totalEvents: relatedEvents.length,
      errorCount: relatedEvents.filter((e) => e.severity === 'error').length,
      warningCount: relatedEvents.filter((e) => e.severity === 'warning').length,
    }
  }
}
