/**
 * DSO Design System
 * Premium SaaS UI inspired by Linear, Vercel, Stripe
 */

export const colors = {
  // Primary - Coral/Pink
  primary: {
    50: '#FFF5F7',
    100: '#FFEBF0',
    200: '#FFD7E3',
    300: '#FFC2D6',
    400: '#FFAEC9',
    500: '#FF8FA3',
    600: '#FF6B81',
    700: '#FF5274',
    800: '#E83860',
    900: '#D01E47',
  },

  // Secondary - Blue
  secondary: {
    50: '#F0F7FF',
    100: '#E0EFFE',
    200: '#C2DFFD',
    300: '#A4D0FC',
    400: '#86C1FB',
    500: '#4F8CFF',
    600: '#2563EB',
    700: '#1D4ED8',
    800: '#1E40AF',
    900: '#1E3A8A',
  },

  // Semantic Colors
  success: '#22C55E',
  warning: '#F59E0B',
  danger: '#EF4444',
  info: '#4F8CFF',

  // Neutral
  background: '#F8FAFC',
  card: '#FFFFFF',
  border: '#E2E8F0',
  borderHover: '#CBD5E1',
  text: {
    primary: '#0F172A',
    secondary: '#475569',
    tertiary: '#94A3B8',
    muted: '#CBD5E1',
  },

  // Dark Mode
  dark: {
    background: '#0F172A',
    card: '#1E293B',
    border: '#334155',
    text: '#F1F5F9',
  },
}

export const spacing = {
  xs: '0.25rem',    // 4px
  sm: '0.5rem',     // 8px
  md: '1rem',       // 16px
  lg: '1.5rem',     // 24px
  xl: '2rem',       // 32px
  '2xl': '3rem',    // 48px
  '3xl': '4rem',    // 64px
}

export const borderRadius = {
  sm: '0.375rem',   // 6px
  md: '0.5rem',     // 8px
  lg: '0.75rem',    // 12px
  xl: '1rem',       // 16px
  '2xl': '1.25rem', // 20px
  '3xl': '1.5rem',  // 24px
  full: '9999px',
}

export const shadows = {
  xs: '0 1px 2px 0 rgba(15, 23, 42, 0.05)',
  sm: '0 1px 3px 0 rgba(15, 23, 42, 0.1), 0 1px 2px 0 rgba(15, 23, 42, 0.06)',
  md: '0 4px 6px -1px rgba(15, 23, 42, 0.1), 0 2px 4px -1px rgba(15, 23, 42, 0.06)',
  lg: '0 10px 15px -3px rgba(15, 23, 42, 0.1), 0 4px 6px -2px rgba(15, 23, 42, 0.05)',
  xl: '0 20px 25px -5px rgba(15, 23, 42, 0.1), 0 10px 10px -5px rgba(15, 23, 42, 0.04)',
  '2xl': '0 25px 50px -12px rgba(15, 23, 42, 0.25)',
  none: 'none',
  inner: 'inset 0 2px 4px 0 rgba(15, 23, 42, 0.05)',
}

export const gradients = {
  coral: 'linear-gradient(135deg, #FF6B81 0%, #FF8FA3 100%)',
  coralHover: 'linear-gradient(135deg, #FF5274 0%, #FF7A95 100%)',
  blue: 'linear-gradient(135deg, #2563EB 0%, #4F8CFF 100%)',
  blueHover: 'linear-gradient(135deg, #1D4ED8 0%, #2563EB 100%)',
  subtle: 'linear-gradient(135deg, #F8FAFC 0%, #F1F5F9 100%)',
  success: 'linear-gradient(135deg, #22C55E 0%, #16A34A 100%)',
  warning: 'linear-gradient(135deg, #F59E0B 0%, #D97706 100%)',
  danger: 'linear-gradient(135deg, #EF4444 0%, #DC2626 100%)',
}

export const transitions = {
  fast: 'transition-all 150ms cubic-bezier(0.4, 0, 0.2, 1)',
  base: 'transition-all 200ms cubic-bezier(0.4, 0, 0.2, 1)',
  slow: 'transition-all 300ms cubic-bezier(0.4, 0, 0.2, 1)',
}

export const typography = {
  fontFamily: 'Inter, system-ui, sans-serif',

  // Heading 1
  h1: {
    fontSize: '2rem',      // 32px
    fontWeight: 700,
    lineHeight: 1.2,
    letterSpacing: '-0.02em',
  },

  // Heading 2
  h2: {
    fontSize: '1.5rem',    // 24px
    fontWeight: 700,
    lineHeight: 1.3,
    letterSpacing: '-0.01em',
  },

  // Heading 3
  h3: {
    fontSize: '1.25rem',   // 20px
    fontWeight: 600,
    lineHeight: 1.4,
    letterSpacing: '-0.01em',
  },

  // Heading 4
  h4: {
    fontSize: '1rem',      // 16px
    fontWeight: 600,
    lineHeight: 1.5,
  },

  // Body Large
  bodyLg: {
    fontSize: '1rem',      // 16px
    fontWeight: 400,
    lineHeight: 1.5,
  },

  // Body
  body: {
    fontSize: '0.9375rem', // 15px
    fontWeight: 400,
    lineHeight: 1.5,
  },

  // Body Small
  bodySm: {
    fontSize: '0.875rem',  // 14px
    fontWeight: 400,
    lineHeight: 1.5,
  },

  // Label
  label: {
    fontSize: '0.875rem',  // 14px
    fontWeight: 500,
    lineHeight: 1.5,
    textTransform: 'capitalize',
  },

  // Caption
  caption: {
    fontSize: '0.75rem',   // 12px
    fontWeight: 500,
    lineHeight: 1.4,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
  },
}

// Component-specific utilities
export const componentStyles = {
  // Button variants
  button: {
    primary: {
      bg: colors.primary[600],
      bgHover: colors.primary[700],
      text: '#FFFFFF',
      shadow: shadows.md,
    },
    secondary: {
      bg: colors.card,
      bgHover: colors.background,
      text: colors.text.primary,
      border: colors.border,
      shadow: shadows.sm,
    },
    danger: {
      bg: colors.danger,
      bgHover: '#DC2626',
      text: '#FFFFFF',
      shadow: shadows.md,
    },
    ghost: {
      bg: 'transparent',
      bgHover: colors.background,
      text: colors.text.primary,
    },
  },

  // Card styles
  card: {
    bg: colors.card,
    border: colors.border,
    rounded: borderRadius['2xl'],
    shadow: shadows.sm,
    shadowHover: shadows.md,
    padding: spacing.lg,
  },

  // Input styles
  input: {
    bg: colors.card,
    border: colors.border,
    borderHover: colors.borderHover,
    borderFocus: colors.primary[600],
    rounded: borderRadius.lg,
    padding: `${spacing.sm} ${spacing.md}`,
  },

  // Badge styles
  badge: {
    success: {
      bg: '#DCFCE7',
      text: '#166534',
      border: '#86EFAC',
    },
    warning: {
      bg: '#FEF3C7',
      text: '#92400E',
      border: '#FDE047',
    },
    danger: {
      bg: '#FEE2E2',
      text: '#991B1B',
      border: '#FCA5A5',
    },
    info: {
      bg: '#DBEAFE',
      text: '#1E40AF',
      border: '#93C5FD',
    },
    default: {
      bg: '#F1F5F9',
      text: '#0F172A',
      border: '#CBD5E1',
    },
  },
}

// Animation utilities
export const animations = {
  fadeIn: 'fade-in 300ms ease-in',
  slideInUp: 'slide-in-up 300ms ease-out',
  slideInDown: 'slide-in-down 300ms ease-out',
  scaleIn: 'scale-in 200ms ease-out',
  pulse: 'pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
}
