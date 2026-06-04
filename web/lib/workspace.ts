/**
 * Workspace Engine
 *
 * Maintains a draft configuration model in browser memory.
 * Allows operators to simulate configuration changes without persistence.
 *
 * All operations are non-destructive and exist only in memory.
 */

export interface WorkspaceMapping {
  id: string
  container: string
  secret: string
}

export interface WorkspaceSecret {
  id: string
  name: string
  provider: string
  status: string
}

export interface WorkspaceConfig {
  id: string
  createdAt: string
  mappings: WorkspaceMapping[]
  secrets: WorkspaceSecret[]
  metadata: {
    isDraft: true
    isApplied: false
    fromChangeSetId?: string
    fromRemediationPlanId?: string
  }
}

export interface WorkspaceChange {
  type: 'add_mapping' | 'remove_mapping' | 'add_secret' | 'remove_secret'
  target: WorkspaceMapping | WorkspaceSecret
  timestamp: string
}

export interface WorkspaceState {
  config: WorkspaceConfig
  changes: WorkspaceChange[]
  isModified: boolean
}

/**
 * Create empty workspace
 */
export function createWorkspace(): WorkspaceState {
  return {
    config: {
      id: `workspace-${Date.now()}`,
      createdAt: new Date().toISOString(),
      mappings: [],
      secrets: [],
      metadata: {
        isDraft: true,
        isApplied: false,
      },
    },
    changes: [],
    isModified: false,
  }
}

/**
 * Initialize workspace from current configuration
 */
export function initializeWorkspaceFromCurrent(
  containers: any[],
  secrets: any[],
  mappings: Array<{ container: string; secret: string }>
): WorkspaceState {
  const workspace = createWorkspace()

  // Add current secrets
  secrets.forEach((secret) => {
    workspace.config.secrets.push({
      id: `secret-${secret.name}`,
      name: secret.name,
      provider: secret.provider,
      status: secret.status,
    })
  })

  // Add current mappings
  mappings.forEach((mapping, index) => {
    workspace.config.mappings.push({
      id: `mapping-${index}`,
      container: mapping.container,
      secret: mapping.secret,
    })
  })

  return workspace
}

/**
 * Add mapping to draft
 */
export function addMapping(
  workspace: WorkspaceState,
  container: string,
  secret: string
): WorkspaceState {
  const newMapping: WorkspaceMapping = {
    id: `mapping-${Date.now()}`,
    container,
    secret,
  }

  const newChange: WorkspaceChange = {
    type: 'add_mapping',
    target: newMapping,
    timestamp: new Date().toISOString(),
  }

  return {
    ...workspace,
    config: {
      ...workspace.config,
      mappings: [...workspace.config.mappings, newMapping],
    },
    changes: [...workspace.changes, newChange],
    isModified: true,
  }
}

/**
 * Remove mapping from draft
 */
export function removeMapping(
  workspace: WorkspaceState,
  mappingId: string
): WorkspaceState {
  const mapping = workspace.config.mappings.find((m) => m.id === mappingId)
  if (!mapping) return workspace

  const newChange: WorkspaceChange = {
    type: 'remove_mapping',
    target: mapping,
    timestamp: new Date().toISOString(),
  }

  return {
    ...workspace,
    config: {
      ...workspace.config,
      mappings: workspace.config.mappings.filter((m) => m.id !== mappingId),
    },
    changes: [...workspace.changes, newChange],
    isModified: true,
  }
}

/**
 * Add secret reference to draft
 */
export function addSecret(
  workspace: WorkspaceState,
  name: string,
  provider: string
): WorkspaceState {
  const newSecret: WorkspaceSecret = {
    id: `secret-${Date.now()}`,
    name,
    provider,
    status: 'draft',
  }

  const newChange: WorkspaceChange = {
    type: 'add_secret',
    target: newSecret,
    timestamp: new Date().toISOString(),
  }

  return {
    ...workspace,
    config: {
      ...workspace.config,
      secrets: [...workspace.config.secrets, newSecret],
    },
    changes: [...workspace.changes, newChange],
    isModified: true,
  }
}

/**
 * Remove secret from draft
 */
export function removeSecret(
  workspace: WorkspaceState,
  secretId: string
): WorkspaceState {
  const secret = workspace.config.secrets.find((s) => s.id === secretId)
  if (!secret) return workspace

  const newChange: WorkspaceChange = {
    type: 'remove_secret',
    target: secret,
    timestamp: new Date().toISOString(),
  }

  return {
    ...workspace,
    config: {
      ...workspace.config,
      secrets: workspace.config.secrets.filter((s) => s.id !== secretId),
    },
    changes: [...workspace.changes, newChange],
    isModified: true,
  }
}

/**
 * Apply change set to workspace
 */
export function applyChangeSetToWorkspace(
  workspace: WorkspaceState,
  changeSetId: string,
  changes: Array<{ type: string; container?: string; secret?: string }>
): WorkspaceState {
  let updated = { ...workspace }

  changes.forEach((change) => {
    if (change.type === 'add_mapping' && change.container && change.secret) {
      updated = addMapping(updated, change.container, change.secret)
    } else if (change.type === 'remove_mapping') {
      // Find and remove
      const toRemove = updated.config.mappings.find(
        (m) => m.container === change.container && m.secret === change.secret
      )
      if (toRemove) {
        updated = removeMapping(updated, toRemove.id)
      }
    } else if (change.type === 'add_secret' && change.secret) {
      updated = addSecret(updated, change.secret, 'draft')
    }
  })

  return {
    ...updated,
    config: {
      ...updated.config,
      metadata: {
        ...updated.config.metadata,
        fromChangeSetId: changeSetId,
      },
    },
  }
}

/**
 * Clear all draft changes
 */
export function clearWorkspace(workspace: WorkspaceState): WorkspaceState {
  return {
    ...createWorkspace(),
    config: {
      ...workspace.config,
      mappings: [],
      secrets: [],
    },
    changes: [],
    isModified: false,
  }
}

/**
 * Reset to current configuration
 */
export function resetWorkspace(
  workspace: WorkspaceState,
  containers: any[],
  secrets: any[],
  mappings: Array<{ container: string; secret: string }>
): WorkspaceState {
  return initializeWorkspaceFromCurrent(containers, secrets, mappings)
}

/**
 * Export configuration as JSON
 */
export function exportAsJSON(workspace: WorkspaceState): string {
  const config = {
    version: '1.0',
    timestamp: new Date().toISOString(),
    isDraft: true,
    mappings: workspace.config.mappings.map((m) => ({
      container: m.container,
      secret: m.secret,
    })),
    secrets: workspace.config.secrets.map((s) => ({
      name: s.name,
      provider: s.provider,
    })),
  }
  return JSON.stringify(config, null, 2)
}

/**
 * Export configuration as YAML
 */
export function exportAsYAML(workspace: WorkspaceState): string {
  let yaml = `# Draft Configuration
# Generated: ${new Date().toISOString()}
# This is a DRAFT - not applied to system

version: '1.0'
draft: true

mappings:\n`

  workspace.config.mappings.forEach((mapping) => {
    yaml += `  - container: ${mapping.container}\n`
    yaml += `    secret: ${mapping.secret}\n`
  })

  yaml += `\nsecrets:\n`
  workspace.config.secrets.forEach((secret) => {
    yaml += `  - name: ${secret.name}\n`
    yaml += `    provider: ${secret.provider}\n`
  })

  return yaml
}

/**
 * Download draft configuration
 */
export function downloadDraft(workspace: WorkspaceState, format: 'json' | 'yaml'): void {
  const content = format === 'json' ? exportAsJSON(workspace) : exportAsYAML(workspace)
  const mimeType = format === 'json' ? 'application/json' : 'text/yaml'
  const filename = `dso-draft-${new Date().toISOString().split('T')[0]}.${format === 'json' ? 'json' : 'yaml'}`

  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

/**
 * Get summary statistics
 */
export function getWorkspaceSummary(workspace: WorkspaceState) {
  return {
    mappingCount: workspace.config.mappings.length,
    secretCount: workspace.config.secrets.length,
    changeCount: workspace.changes.length,
    isModified: workspace.isModified,
    addedMappings: workspace.changes.filter((c) => c.type === 'add_mapping').length,
    removedMappings: workspace.changes.filter((c) => c.type === 'remove_mapping').length,
    addedSecrets: workspace.changes.filter((c) => c.type === 'add_secret').length,
    removedSecrets: workspace.changes.filter((c) => c.type === 'remove_secret').length,
  }
}

/**
 * Get diff between two configurations
 */
export function getConfigDiff(
  current: Array<{ container?: string; secret?: string; name?: string; provider?: string }>,
  draft: Array<{ container?: string; secret?: string; name?: string; provider?: string }>
): {
  added: typeof draft
  removed: typeof draft
  unchanged: typeof draft
} {
  const added = draft.filter(
    (d) => !current.some((c) => JSON.stringify(c) === JSON.stringify(d))
  )

  const removed = current.filter(
    (c) => !draft.some((d) => JSON.stringify(d) === JSON.stringify(c))
  )

  const unchanged = current.filter(
    (c) => draft.some((d) => JSON.stringify(d) === JSON.stringify(c))
  )

  return { added, removed, unchanged }
}
