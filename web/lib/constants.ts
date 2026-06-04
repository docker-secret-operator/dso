// API Configuration
// Note: Actual URLs are determined at runtime by the API client
// Dashboard server (default :8472) proxies /api/* requests to REST API (:8471)
export const API_BASE_URL = 'http://127.0.0.1:8472' // Dashboard server
export const API_TIMEOUT = 10000 // 10 seconds

// WebSocket Configuration
export const WS_URL = 'ws://127.0.0.1:8472' // Dashboard server
export const WS_RECONNECT_DELAY = 3000 // 3 seconds
export const WS_MAX_RECONNECT_DELAY = 30000 // 30 seconds
export const WS_MAX_MESSAGE_HISTORY = 100

// Query Configuration
export const QUERY_REFRESH_INTERVAL = 5000 // 5 seconds
export const QUERY_STALE_TIME = 60000 // 1 minute

// UI Configuration
export const SIDEBAR_COLLAPSED_WIDTH = 64
export const SIDEBAR_EXPANDED_WIDTH = 256

// Status Colors
export const STATUS_COLORS = {
  up: 'bg-green-500',
  down: 'bg-red-500',
  pending: 'bg-yellow-500',
  ok: 'bg-green-500',
  error: 'bg-red-500',
  warning: 'bg-yellow-500',
} as const

// Severity Levels
export const SEVERITY_LEVELS = ['info', 'warning', 'error'] as const

// Navigation Items
export const NAV_ITEMS = [
  { name: 'Dashboard', href: '/dashboard', icon: 'BarChart3' },
  { name: 'Secrets', href: '/secrets', icon: 'Lock' },
  { name: 'Discovery', href: '/discovery', icon: 'Server' },
  { name: 'Events', href: '/events', icon: 'Bell' },
  { name: 'Audit Logs', href: '/audit', icon: 'FileText' },
  { name: 'Configuration', href: '/configuration', icon: 'Settings' },
] as const
