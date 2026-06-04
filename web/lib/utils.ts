import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Format timestamp to human-readable string
export function formatTime(timestamp: string | undefined): string {
  if (!timestamp) return 'Never'
  try {
    const date = new Date(timestamp)
    return date.toLocaleString()
  } catch {
    return 'Invalid date'
  }
}

// Format time relative to now (e.g., "2 hours ago")
export function formatRelativeTime(timestamp: string | undefined): string {
  if (!timestamp) return 'Never'
  try {
    const date = new Date(timestamp)
    const now = new Date()
    const diff = now.getTime() - date.getTime()

    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    const days = Math.floor(hours / 24)

    if (seconds < 60) return `${seconds}s ago`
    if (minutes < 60) return `${minutes}m ago`
    if (hours < 24) return `${hours}h ago`
    if (days < 30) return `${days}d ago`
    return formatTime(timestamp)
  } catch {
    return 'Unknown'
  }
}

// Format duration in milliseconds
export function formatDuration(ms: number | undefined): string {
  if (ms === undefined) return '-'
  if (ms < 1000) return `${Math.round(ms)}ms`

  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(1)}s`

  const minutes = seconds / 60
  if (minutes < 60) return `${minutes.toFixed(1)}m`

  const hours = minutes / 60
  return `${hours.toFixed(1)}h`
}

// Format bytes to human-readable size
export function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined) return '-'
  if (bytes === 0) return '0 B'

  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))

  return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
}

// Truncate string to specified length
export function truncate(str: string, length: number): string {
  if (str.length <= length) return str
  return str.substring(0, length) + '...'
}

// Format provider name for display
export function formatProviderName(provider: string): string {
  const names: Record<string, string> = {
    aws: 'AWS Secrets Manager',
    azure: 'Azure Key Vault',
    vault: 'HashiCorp Vault',
    local: 'Local Encrypted',
  }
  return names[provider] || provider
}

// Get status badge color
export function getStatusColor(status: string): string {
  const colors: Record<string, string> = {
    ok: 'bg-green-100 text-green-800 border-green-300',
    pending: 'bg-yellow-100 text-yellow-800 border-yellow-300',
    error: 'bg-red-100 text-red-800 border-red-300',
    up: 'bg-green-100 text-green-800 border-green-300',
    down: 'bg-red-100 text-red-800 border-red-300',
    success: 'bg-green-100 text-green-800 border-green-300',
    failure: 'bg-red-100 text-red-800 border-red-300',
    warning: 'bg-yellow-100 text-yellow-800 border-yellow-300',
    info: 'bg-blue-100 text-blue-800 border-blue-300',
  }
  return colors[status] || 'bg-gray-100 text-gray-800 border-gray-300'
}

// Get severity badge color
export function getSeverityColor(severity: string): string {
  const colors: Record<string, string> = {
    info: 'bg-blue-100 text-blue-800 border-blue-300',
    warning: 'bg-yellow-100 text-yellow-800 border-yellow-300',
    error: 'bg-red-100 text-red-800 border-red-300',
  }
  return colors[severity] || 'bg-gray-100 text-gray-800 border-gray-300'
}
