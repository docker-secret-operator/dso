/**
 * DSO Frontend API - Single export point for all API services
 * All pages import from here
 */

// Export all types
export * from './types'

// Export all services
export * as auth from './auth'
export * as system from './system'
export * as audit from './audit'
export * as discovery from './discovery'
export * as execution from './execution'
export * as operations from './operations'
export * as metrics from './metrics'
export * as users from './users'
export * as dashboard from './dashboard'
export * as drift from './drift'
export * as bulk from './bulk'

// Re-export apiClient for direct use if needed
export { apiClient } from '../api-client'
