// Tech Noir Color Palette
// Inspired by Grafana Cloud, Datadog, Linear, Vercel

export const colors = {
  // Base neutrals
  background: {
    primary: '#0B1020',    // Darkest - main bg
    secondary: '#111827',  // Surface - cards, panels
    tertiary: '#1A2235',   // Elevated - hover states
    overlay: 'rgba(0, 0, 0, 0.4)',
  },

  // Semantic colors
  primary: '#3B82F6',      // Blue - primary actions
  accent: '#22D3EE',       // Cyan - highlights, secondary
  success: '#10B981',      // Green - success states
  warning: '#F59E0B',      // Amber - warnings
  danger: '#EF4444',       // Red - errors, critical
  info: '#3B82F6',         // Blue - info

  // Text colors
  text: {
    primary: '#F9FAFB',    // White text
    secondary: '#9CA3AF',  // Gray text - muted labels
    muted: '#6B7280',      // Darkest text - disabled
    inverse: '#0B1020',    // For light backgrounds (rare)
  },

  // Borders
  border: {
    default: 'rgba(255, 255, 255, 0.08)',
    subtle: 'rgba(255, 255, 255, 0.03)',
    strong: 'rgba(255, 255, 255, 0.12)',
  },

  // Status-specific
  status: {
    success: {
      bg: 'rgba(16, 185, 129, 0.1)',
      border: 'rgba(16, 185, 129, 0.3)',
      text: '#10B981',
    },
    warning: {
      bg: 'rgba(245, 158, 11, 0.1)',
      border: 'rgba(245, 158, 11, 0.3)',
      text: '#F59E0B',
    },
    danger: {
      bg: 'rgba(239, 68, 68, 0.1)',
      border: 'rgba(239, 68, 68, 0.3)',
      text: '#EF4444',
    },
    info: {
      bg: 'rgba(59, 130, 246, 0.1)',
      border: 'rgba(59, 130, 246, 0.3)',
      text: '#3B82F6',
    },
  },
}
