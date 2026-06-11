/**
 * DSO Premium Design System Tokens
 * Enterprise-grade design language inspired by Linear, Vercel, Stripe, Datadog
 */

// ============================================================================
// COLORS
// ============================================================================

export const colors = {
  // Grayscale Foundation
  slate: {
    50: '#F8FAFC',
    100: '#F1F5F9',
    200: '#E2E8F0',
    300: '#CBD5E1',
    400: '#94A3B8',
    500: '#64748B',
    600: '#475569',
    700: '#334155',
    800: '#1E293B',
    900: '#0F172A',
  },

  // Primary: Indigo (Premium, Trust, Intelligence)
  indigo: {
    50: '#EEF2FF',
    100: '#E0E7FF',
    500: '#6366F1',
    600: '#4F46E5',
    700: '#4338CA',
    900: '#312E81',
  },

  // Incidents: Orange-Red (Urgent, Attention)
  incident: {
    50: '#FFF7ED',
    100: '#FFEDD5',
    500: '#F97316',
    600: '#EA580C',
    700: '#C2410C',
    900: '#7C2D12',
  },

  // Recommendations: Purple (Smart, Insights)
  recommendation: {
    50: '#FAF5FF',
    100: '#F3E8FF',
    500: '#A855F7',
    600: '#9333EA',
    700: '#7E22CE',
    900: '#581C87',
  },

  // Forecasts: Cyan (Future, Predictive)
  forecast: {
    50: '#ECFDFD',
    100: '#CFFAFE',
    500: '#06B6D4',
    600: '#0891B2',
    700: '#0E7490',
    900: '#082F4B',
  },

  // Drift: Amber (Warning, Change)
  drift: {
    50: '#FFFBEB',
    100: '#FEF3C7',
    500: '#F59E0B',
    600: '#D97706',
    700: '#B45309',
    900: '#78350F',
  },

  // Autonomy: Emerald (Action, Automation)
  autonomy: {
    50: '#F0FDF4',
    100: '#DCFCE7',
    500: '#10B981',
    600: '#059669',
    700: '#047857',
    900: '#064E3B',
  },

  // Security: Blue (Protection, Lock)
  security: {
    50: '#EFF6FF',
    100: '#DBEAFE',
    500: '#3B82F6',
    600: '#2563EB',
    700: '#1D4ED8',
    900: '#1E3A8A',
  },

  // Policies: Indigo (Governance)
  policy: {
    50: '#EEF2FF',
    100: '#E0E7FF',
    500: '#6366F1',
    600: '#4F46E5',
    700: '#4338CA',
    900: '#312E81',
  },

  // Plugins: Slate (Extensible)
  plugin: {
    50: '#F8FAFC',
    100: '#F1F5F9',
    500: '#64748B',
    600: '#475569',
    700: '#334155',
    900: '#0F172A',
  },

  // Alerts: Red (Critical, Error)
  alert: {
    50: '#FEF2F2',
    100: '#FEE2E2',
    500: '#EF4444',
    600: '#DC2626',
    700: '#B91C1C',
    900: '#7F1D1D',
  },

  // Success: Green
  success: {
    50: '#F0FDF4',
    100: '#DCFCE7',
    500: '#22C55E',
    600: '#16A34A',
    700: '#15803D',
    900: '#166534',
  },

  // Warning: Yellow
  warning: {
    50: '#FEFCE8',
    100: '#FEF3C7',
    500: '#EAB308',
    600: '#CA8A04',
    700: '#A16207',
    900: '#713F12',
  },

  // Danger: Red
  danger: {
    50: '#FEF2F2',
    100: '#FEE2E2',
    500: '#EF4444',
    600: '#DC2626',
    700: '#B91C1C',
    900: '#7F1D1D',
  },
}

// ============================================================================
// TYPOGRAPHY
// ============================================================================

export const typography = {
  fontFamily: {
    base: 'ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    mono: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
  },

  sizes: {
    // Headings
    h1: { size: '56px', weight: 800, lineHeight: '1.1', letterSpacing: '-0.02em' },
    h2: { size: '40px', weight: 700, lineHeight: '1.2', letterSpacing: '-0.01em' },
    h3: { size: '28px', weight: 700, lineHeight: '1.3' },
    h4: { size: '24px', weight: 700, lineHeight: '1.3' },
    h5: { size: '20px', weight: 600, lineHeight: '1.4' },
    h6: { size: '18px', weight: 600, lineHeight: '1.4' },

    // Body
    bodyLg: { size: '16px', weight: 400, lineHeight: '1.6' },
    body: { size: '15px', weight: 400, lineHeight: '1.6' },
    bodySm: { size: '14px', weight: 400, lineHeight: '1.5' },
    bodyXs: { size: '12px', weight: 400, lineHeight: '1.5' },

    // Labels
    labelLg: { size: '16px', weight: 600, lineHeight: '1.5' },
    label: { size: '14px', weight: 600, lineHeight: '1.5' },
    labelSm: { size: '12px', weight: 600, lineHeight: '1.4' },

    // Captions
    caption: { size: '12px', weight: 500, lineHeight: '1.4' },
    captionSm: { size: '11px', weight: 500, lineHeight: '1.4' },

    // Code
    code: { size: '13px', weight: 400, lineHeight: '1.5' },
  },
}

// ============================================================================
// SPACING
// ============================================================================

export const spacing = {
  xs: '4px',
  sm: '8px',
  md: '12px',
  lg: '16px',
  xl: '24px',
  '2xl': '32px',
  '3xl': '48px',
  '4xl': '64px',
}

// ============================================================================
// BORDER RADIUS
// ============================================================================

export const borderRadius = {
  sm: '6px',
  md: '8px',
  lg: '12px',
  xl: '16px',
  '2xl': '20px', // Cards
  '3xl': '24px',
  full: '9999px',
}

// ============================================================================
// SHADOWS
// ============================================================================

export const shadows = {
  none: 'none',
  xs: '0 1px 2px 0 rgba(0, 0, 0, 0.05)',
  sm: '0 1px 3px 0 rgba(0, 0, 0, 0.1), 0 1px 2px 0 rgba(0, 0, 0, 0.06)',
  md: '0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06)',
  lg: '0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05)',
  xl: '0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04)',
  '2xl': '0 25px 50px -12px rgba(0, 0, 0, 0.25)',
}

// ============================================================================
// TRANSITIONS
// ============================================================================

export const transitions = {
  fast: '150ms ease-out',
  base: '200ms ease-out',
  slow: '300ms ease-out',
}

// ============================================================================
// GRADIENTS (SUBSYSTEM IDENTITIES)
// ============================================================================

export const gradients = {
  // Incidents
  incidentSoft: 'from-incident-500/10 to-incident-400/5',
  incidentGlow: 'from-incident-600 to-incident-700',

  // Recommendations
  recommendationSoft: 'from-recommendation-500/10 to-recommendation-400/5',
  recommendationGlow: 'from-recommendation-600 to-recommendation-700',

  // Forecasts
  forecastSoft: 'from-forecast-500/10 to-forecast-400/5',
  forecastGlow: 'from-forecast-600 to-forecast-700',

  // Drift
  driftSoft: 'from-drift-500/10 to-drift-400/5',
  driftGlow: 'from-drift-600 to-drift-700',

  // Autonomy
  autonomySoft: 'from-autonomy-500/10 to-autonomy-400/5',
  autonomyGlow: 'from-autonomy-600 to-autonomy-700',

  // Security
  securitySoft: 'from-security-500/10 to-security-400/5',
  securityGlow: 'from-security-600 to-security-700',

  // Premium (default)
  premiumSoft: 'from-indigo-500/10 to-indigo-400/5',
  premiumGlow: 'from-indigo-600 to-indigo-700',
}

// ============================================================================
// COMPONENT-SPECIFIC UTILITIES
// ============================================================================

export const components = {
  // Card
  card: {
    default: 'rounded-2xl border border-slate-200 bg-white shadow-sm',
    gradient: 'rounded-2xl border border-slate-200 bg-gradient-to-br from-white to-slate-50 shadow-sm',
    bordered: 'rounded-2xl border-2 border-slate-300 bg-white shadow-none',
    elevated: 'rounded-2xl border border-slate-200 bg-white shadow-lg',
  },

  // Button
  button: {
    primary: 'rounded-lg bg-gradient-to-r from-indigo-600 to-indigo-700 text-white shadow-md hover:shadow-lg focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500',
    secondary: 'rounded-lg bg-white border border-slate-300 text-slate-900 shadow-sm hover:bg-slate-50 focus:ring-2 focus:ring-offset-2 focus:ring-slate-500',
    danger: 'rounded-lg bg-red-600 text-white shadow-md hover:shadow-lg focus:ring-2 focus:ring-offset-2 focus:ring-red-500',
    ghost: 'rounded-lg bg-transparent text-slate-700 hover:bg-slate-100 focus:ring-2 focus:ring-offset-2 focus:ring-slate-500',
  },

  // Input
  input: 'rounded-lg border border-slate-300 bg-white px-4 py-2.5 text-slate-900 focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20',

  // Badge
  badge: {
    success: 'rounded-full bg-success-100 text-success-800 px-3 py-1 text-sm font-medium',
    warning: 'rounded-full bg-warning-100 text-warning-800 px-3 py-1 text-sm font-medium',
    danger: 'rounded-full bg-danger-100 text-danger-800 px-3 py-1 text-sm font-medium',
    info: 'rounded-full bg-slate-200 text-slate-800 px-3 py-1 text-sm font-medium',
  },
}

// ============================================================================
// SUBSYSTEM COLOR MAP
// ============================================================================

export const subsystemColors = {
  incidents: colors.incident,
  recommendations: colors.recommendation,
  forecasts: colors.forecast,
  drift: colors.drift,
  autonomy: colors.autonomy,
  security: colors.security,
  policies: colors.policy,
  plugins: colors.plugin,
  alerts: colors.alert,
} as const

export type SubsystemKey = keyof typeof subsystemColors

export function getSubsystemColor(subsystem: SubsystemKey, shade: number = 600): string {
  const shadeKey = shade as keyof typeof subsystemColors[typeof subsystem]
  return subsystemColors[subsystem][shadeKey] as string
}
