// Shadow definitions - subtle depth, no neon glows
export const shadows = {
  // Elevation levels for depth
  sm: '0 1px 2px 0 rgba(0, 0, 0, 0.05)',
  base: '0 1px 3px 0 rgba(0, 0, 0, 0.1), 0 1px 2px 0 rgba(0, 0, 0, 0.06)',
  md: '0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06)',
  lg: '0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05)',
  xl: '0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04)',

  // Card shadow - used for cards and elevated surfaces
  card: `
    0 0 0 1px rgba(255, 255, 255, 0.03),
    0 8px 24px rgba(0, 0, 0, 0.3)
  `,

  // Interactive shadow - used for hover/active states
  interactive: '0 0 0 1px rgba(255, 255, 255, 0.08), 0 4px 12px rgba(0, 0, 0, 0.2)',

  // Focus ring shadow - for accessibility
  focus: '0 0 0 3px rgba(59, 130, 246, 0.1), 0 0 0 1px rgba(59, 130, 246, 0.5)',
}
