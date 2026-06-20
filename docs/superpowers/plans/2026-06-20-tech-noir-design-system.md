# Tech Noir Design System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evolve the DSO dashboard into a polished Tech Noir design system with centralized tokens, refined component styling, and enterprise-grade visual quality (9.5/10) while preserving all functionality.

**Architecture:** Create a centralized design token system (colors, spacing, shadows, typography) in `web/lib/design-system/`, update Tailwind config to consume these tokens, apply Tech Noir palette via CSS variables, and refine component styling systematically without changing layouts or functionality.

**Tech Stack:** Next.js, Tailwind CSS, TypeScript, Lucide icons

---

## Task 1: Create Design System Foundation - Colors

**Files:**
- Create: `web/lib/design-system/colors.ts`
- Test: Verify TypeScript compilation

- [ ] **Step 1: Create colors.ts with Tech Noir palette**

Create the file at `web/lib/design-system/colors.ts`:

```typescript
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
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd web && npm run type-check`

Expected: No TypeScript errors in the new file.

- [ ] **Step 3: Commit**

```bash
git add web/lib/design-system/colors.ts
git commit -m "feat: define Tech Noir color palette"
```

---

## Task 2: Create Design System - Spacing, Radius, Typography

**Files:**
- Create: `web/lib/design-system/spacing.ts`
- Create: `web/lib/design-system/radius.ts`
- Create: `web/lib/design-system/typography.ts`

- [ ] **Step 1: Create spacing.ts**

Create the file at `web/lib/design-system/spacing.ts`:

```typescript
// Spacing scale - follows 4px base unit
export const spacing = {
  xs: '4px',
  sm: '8px',
  md: '12px',
  lg: '16px',
  xl: '20px',
  '2xl': '24px',
  '3xl': '32px',
  '4xl': '40px',
  '5xl': '48px',
  '6xl': '64px',
}
```

- [ ] **Step 2: Create radius.ts**

Create the file at `web/lib/design-system/radius.ts`:

```typescript
// Border radius tokens - prefer 12px as default
export const radius = {
  none: '0',
  sm: '6px',
  md: '12px',        // Default for most components
  lg: '16px',        // Larger containers
  full: '9999px',    // Pills, avatars
}
```

- [ ] **Step 3: Create typography.ts**

Create the file at `web/lib/design-system/typography.ts`:

```typescript
// Typography scale - for semantic sizing
export const typography = {
  // Font sizes (base 14px for normal text)
  fontSize: {
    xs: '12px',      // Labels, captions
    sm: '13px',      // Secondary text
    base: '14px',    // Body text (default)
    lg: '16px',      // Subheadings
    xl: '18px',      // Section headers
    '2xl': '22px',   // Page titles
    '3xl': '28px',   // Large titles
    '4xl': '36px',   // Hero text
  },

  // Font weights
  fontWeight: {
    normal: '400',
    medium: '500',
    semibold: '600',
    bold: '700',
  },

  // Line heights
  lineHeight: {
    tight: '1.2',
    normal: '1.5',
    relaxed: '1.75',
  },

  // Letter spacing
  letterSpacing: {
    tight: '-0.02em',
    normal: '0',
    wide: '0.02em',
  },
}
```

- [ ] **Step 4: Verify TypeScript compilation**

Run: `cd web && npm run type-check`

Expected: All three files compile without errors.

- [ ] **Step 5: Commit**

```bash
git add web/lib/design-system/spacing.ts web/lib/design-system/radius.ts web/lib/design-system/typography.ts
git commit -m "feat: add spacing, radius, and typography tokens"
```

---

## Task 3: Create Design System - Shadows

**Files:**
- Create: `web/lib/design-system/shadows.ts`

- [ ] **Step 1: Create shadows.ts**

Create the file at `web/lib/design-system/shadows.ts`:

```typescript
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
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd web && npm run type-check`

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add web/lib/design-system/shadows.ts
git commit -m "feat: define subtle shadow system for depth"
```

---

## Task 4: Create Design System Index and Tokens Barrel

**Files:**
- Create: `web/lib/design-system/index.ts`
- Create: `web/lib/design-system/tokens.ts`

- [ ] **Step 1: Create tokens.ts**

Create the file at `web/lib/design-system/tokens.ts`:

```typescript
// Central tokens file - aggregates all design system values
import { colors } from './colors'
import { spacing } from './spacing'
import { radius } from './radius'
import { typography } from './typography'
import { shadows } from './shadows'

export const designTokens = {
  colors,
  spacing,
  radius,
  typography,
  shadows,
}

// Named exports for direct access
export { colors, spacing, radius, typography, shadows }
```

- [ ] **Step 2: Create index.ts for barrel export**

Create the file at `web/lib/design-system/index.ts`:

```typescript
// Main export for design system
export * from './tokens'
export { colors } from './colors'
export { spacing } from './spacing'
export { radius } from './radius'
export { typography } from './typography'
export { shadows } from './shadows'
```

- [ ] **Step 3: Verify TypeScript compilation**

Run: `cd web && npm run type-check`

Expected: All design system files compile successfully.

- [ ] **Step 4: Test import in component**

Verify the export works by checking it can be imported. Run:

```bash
cd web && node -e "const ds = require('./lib/design-system/index.ts'); console.log(Object.keys(ds))"
```

Expected: No runtime errors.

- [ ] **Step 5: Commit**

```bash
git add web/lib/design-system/index.ts web/lib/design-system/tokens.ts
git commit -m "feat: create design-system barrel exports"
```

---

## Task 5: Update Globals CSS with Tech Noir Variables

**Files:**
- Modify: `web/app/globals.css`

- [ ] **Step 1: Read current globals.css**

Check the current file to understand the existing structure.

- [ ] **Step 2: Add CSS custom properties for Tech Noir**

Update `web/app/globals.css`. Find the existing `:root {}` or `html {}` section and ensure it includes these variables. If no `:root` exists, add this at the top after any imports:

```css
:root {
  /* Background colors */
  --color-bg-primary: #0B1020;
  --color-bg-secondary: #111827;
  --color-bg-tertiary: #1A2235;
  --color-bg-overlay: rgba(0, 0, 0, 0.4);

  /* Semantic colors */
  --color-primary: #3B82F6;
  --color-accent: #22D3EE;
  --color-success: #10B981;
  --color-warning: #F59E0B;
  --color-danger: #EF4444;
  --color-info: #3B82F6;

  /* Text colors */
  --color-text-primary: #F9FAFB;
  --color-text-secondary: #9CA3AF;
  --color-text-muted: #6B7280;
  --color-text-inverse: #0B1020;

  /* Border colors */
  --color-border-default: rgba(255, 255, 255, 0.08);
  --color-border-subtle: rgba(255, 255, 255, 0.03);
  --color-border-strong: rgba(255, 255, 255, 0.12);

  /* Spacing */
  --spacing-xs: 4px;
  --spacing-sm: 8px;
  --spacing-md: 12px;
  --spacing-lg: 16px;
  --spacing-xl: 20px;
  --spacing-2xl: 24px;
  --spacing-3xl: 32px;

  /* Radius */
  --radius-sm: 6px;
  --radius-md: 12px;
  --radius-lg: 16px;
  --radius-full: 9999px;

  /* Shadows */
  --shadow-sm: 0 1px 2px 0 rgba(0, 0, 0, 0.05);
  --shadow-base: 0 1px 3px 0 rgba(0, 0, 0, 0.1), 0 1px 2px 0 rgba(0, 0, 0, 0.06);
  --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
  --shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05);
  --shadow-card: 0 0 0 1px rgba(255, 255, 255, 0.03), 0 8px 24px rgba(0, 0, 0, 0.3);
  --shadow-focus: 0 0 0 3px rgba(59, 130, 246, 0.1), 0 0 0 1px rgba(59, 130, 246, 0.5);
}
```

- [ ] **Step 3: Verify CSS loads without errors**

Run: `cd web && npm run dev`

Expected: Dev server starts, no CSS compilation errors.

- [ ] **Step 4: Commit**

```bash
git add web/app/globals.css
git commit -m "feat: add Tech Noir CSS custom properties"
```

---

## Task 6: Update Tailwind Config to Use Design Tokens

**Files:**
- Modify: `web/tailwind.config.ts`

- [ ] **Step 1: Read current tailwind.config.ts**

Check the existing Tailwind configuration structure.

- [ ] **Step 2: Extend Tailwind theme with design tokens**

Update `web/tailwind.config.ts`. Find the `theme: { extend: {} }` section and add:

```typescript
import type { Config } from 'tailwindcss'
import { colors, spacing, radius, shadows } from './lib/design-system'

const config: Config = {
  content: [
    './app/**/*.{js,ts,jsx,tsx,mdx}',
    './components/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  theme: {
    extend: {
      colors: {
        // Map design tokens to Tailwind
        'bg-primary': colors.background.primary,
        'bg-secondary': colors.background.secondary,
        'bg-tertiary': colors.background.tertiary,
        'bg-overlay': colors.background.overlay,
        'text-primary': colors.text.primary,
        'text-secondary': colors.text.secondary,
        'text-muted': colors.text.muted,
        'border-default': colors.border.default,
        'border-subtle': colors.border.subtle,
        'border-strong': colors.border.strong,
        // Status colors
        'success': colors.success,
        'warning': colors.warning,
        'danger': colors.danger,
        'info': colors.info,
      },
      spacing: spacing,
      borderRadius: radius,
      boxShadow: {
        'sm': shadows.sm,
        'base': shadows.base,
        'md': shadows.md,
        'lg': shadows.lg,
        'xl': shadows.xl,
        'card': shadows.card,
        'interactive': shadows.interactive,
        'focus': shadows.focus,
      },
      fontSize: {
        'xs': colors.text.primary ? '12px' : '12px',  // Fallback
        'sm': '13px',
        'base': '14px',
        'lg': '16px',
        'xl': '18px',
        '2xl': '22px',
        '3xl': '28px',
      },
    },
  },
  plugins: [],
}

export default config
```

- [ ] **Step 3: Verify TypeScript compilation**

Run: `cd web && npm run type-check`

Expected: No errors in Tailwind config.

- [ ] **Step 4: Test Tailwind build**

Run: `cd web && npm run build`

Expected: Build succeeds, CSS is generated correctly.

- [ ] **Step 5: Commit**

```bash
git add web/tailwind.config.ts
git commit -m "feat: integrate design tokens into Tailwind config"
```

---

## Task 7: Refine Card Component Styling

**Files:**
- Modify: `web/components/ui-modern/Card.tsx`

- [ ] **Step 1: Read current Card.tsx**

Check the existing Card component implementation.

- [ ] **Step 2: Update Card styling with Tech Noir**

Modify `web/components/ui-modern/Card.tsx` to apply refined card styling:

```typescript
import React from 'react'
import { shadows, colors } from '@/lib/design-system'

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: 'default' | 'interactive' | 'elevated'
  children: React.ReactNode
}

export function Card({
  variant = 'default',
  className = '',
  children,
  ...props
}: CardProps) {
  const variants = {
    default: `
      bg-[#111827]
      border border-[rgba(255,255,255,0.08)]
      rounded-[12px]
      p-[20px]
      shadow-[0_0_0_1px_rgba(255,255,255,0.03),_0_8px_24px_rgba(0,0,0,0.3)]
    `,
    interactive: `
      bg-[#111827]
      border border-[rgba(255,255,255,0.08)]
      rounded-[12px]
      p-[20px]
      shadow-[0_0_0_1px_rgba(255,255,255,0.03),_0_8px_24px_rgba(0,0,0,0.3)]
      hover:border-[rgba(255,255,255,0.12)]
      hover:shadow-[0_0_0_1px_rgba(255,255,255,0.08),_0_4px_12px_rgba(0,0,0,0.2)]
      transition-all duration-200
    `,
    elevated: `
      bg-[#1A2235]
      border border-[rgba(255,255,255,0.12)]
      rounded-[12px]
      p-[20px]
      shadow-[0_10px_15px_-3px_rgba(0,0,0,0.1),_0_4px_6px_-2px_rgba(0,0,0,0.05)]
    `,
  }

  return (
    <div
      className={`${variants[variant]} ${className}`.replace(/\s+/g, ' ')}
      {...props}
    >
      {children}
    </div>
  )
}
```

- [ ] **Step 3: Run dev server and visually verify**

Run: `cd web && npm run dev`

Open browser to http://localhost:3000 and check that cards:
- Have subtle depth with proper shadows
- Show proper border definition
- Have adequate padding (20px)
- Display hover states smoothly

Expected: Cards look refined with clear separation from background.

- [ ] **Step 4: Commit**

```bash
git add web/components/ui-modern/Card.tsx
git commit -m "feat: refine card styling with Tech Noir design"
```

---

## Task 8: Create/Refine Badge Component for Status Badges

**Files:**
- Modify or Create: `web/components/ui-modern/Badge.tsx`

- [ ] **Step 1: Check if Badge.tsx exists**

If it doesn't exist, create it. If it exists, read current implementation.

- [ ] **Step 2: Implement standardized status badge component**

Create or update `web/components/ui-modern/Badge.tsx`:

```typescript
import React from 'react'

interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  variant?: 'success' | 'warning' | 'danger' | 'info' | 'default'
  size?: 'sm' | 'md' | 'lg'
  children: React.ReactNode
}

export function Badge({
  variant = 'default',
  size = 'md',
  className = '',
  children,
  ...props
}: BadgeProps) {
  const variants = {
    success: 'bg-[rgba(16,185,129,0.1)] border border-[rgba(16,185,129,0.3)] text-[#10B981]',
    warning: 'bg-[rgba(245,158,11,0.1)] border border-[rgba(245,158,11,0.3)] text-[#F59E0B]',
    danger: 'bg-[rgba(239,68,68,0.1)] border border-[rgba(239,68,68,0.3)] text-[#EF4444]',
    info: 'bg-[rgba(59,130,246,0.1)] border border-[rgba(59,130,246,0.3)] text-[#3B82F6]',
    default: 'bg-[rgba(255,255,255,0.05)] border border-[rgba(255,255,255,0.08)] text-[#9CA3AF]',
  }

  const sizes = {
    sm: 'px-2 py-1 text-xs',
    md: 'px-3 py-1.5 text-sm',
    lg: 'px-4 py-2 text-base',
  }

  return (
    <span
      className={`
        inline-flex
        items-center
        rounded-full
        font-medium
        ${variants[variant]}
        ${sizes[size]}
        ${className}
      `.replace(/\s+/g, ' ')}
      {...props}
    >
      {children}
    </span>
  )
}
```

- [ ] **Step 3: Update Badge exports in ui-modern/index.ts**

Update `web/components/ui-modern/index.ts` to include:

```typescript
export { Badge } from './Badge'
```

- [ ] **Step 4: Verify component renders correctly**

Run: `cd web && npm run dev`

Open a page that uses badges (e.g., Discovery page with status indicators) and verify:
- Status colors match Tech Noir palette
- Badges have proper spacing
- Hover/active states are clear

Expected: Status badges display consistently across the app.

- [ ] **Step 5: Commit**

```bash
git add web/components/ui-modern/Badge.tsx web/components/ui-modern/index.ts
git commit -m "feat: create standardized status badge component"
```

---

## Task 9: Replace Hardcoded Colors in Key Components

**Files:**
- Modify: Components using hardcoded colors (Dashboard, Audit, Discovery pages)

- [ ] **Step 1: Find components with hardcoded colors**

Run: `cd web && grep -r "bg-\[#" components/ app/ --include="*.tsx" | head -20`

This shows components using hardcoded hex colors.

- [ ] **Step 2: Update Dashboard page header colors**

Find `web/app/dashboard/page.tsx` and replace hardcoded colors:

Search for hex colors like `#0a0b0f`, `#111318` and replace with CSS variables:

```typescript
// Before: className="bg-[#0a0b0f]"
// After:  className="bg-[#0B1020]"  // Using primary background

// Before: className="text-slate-100"
// After:  className="text-[#F9FAFB]"  // Using text-primary
```

- [ ] **Step 3: Update Discovery page colors**

Update `web/app/discovery/page.tsx`:

- Replace `bg-[#0a0b0f]` with `bg-[#0B1020]`
- Replace `bg-[#111318]` with `bg-[#111827]`
- Replace `border-white/[0.09]` with `border-[rgba(255,255,255,0.08)]`
- Update status colors to match palette

- [ ] **Step 4: Update Audit page colors**

Apply same updates to `web/app/audit/page.tsx` and related audit components.

- [ ] **Step 5: Test dev server**

Run: `cd web && npm run dev`

Navigate through pages and verify:
- Colors are consistent
- No visual breaks
- Cards, buttons, text all use correct palette

Expected: UI looks cohesive with Tech Noir palette throughout.

- [ ] **Step 6: Commit**

```bash
git add web/app/dashboard/page.tsx web/app/discovery/page.tsx web/app/audit/page.tsx
git commit -m "feat: replace hardcoded colors with Tech Noir palette"
```

---

## Task 10: Enhance Typography Hierarchy

**Files:**
- Modify: Key component files with text elements

- [ ] **Step 1: Review typography in Dashboard**

Check `web/app/dashboard/page.tsx` for heading and text styles.

- [ ] **Step 2: Apply typography hierarchy**

Update heading and text sizing:

```typescript
// Page titles - use larger, semibold
<h1 className="text-[28px] font-semibold text-[#F9FAFB]">
  Dashboard
</h1>

// Section headers - medium size
<h2 className="text-[18px] font-semibold text-[#F9FAFB] mb-4">
  System Health
</h2>

// Subsection headers - base size, semibold
<h3 className="text-[14px] font-semibold text-[#F9FAFB] mb-2">
  Key Metrics
</h3>

// Body text - base, normal weight
<p className="text-[14px] font-normal text-[#9CA3AF]">
  This is secondary information
</p>

// Labels - small, semibold
<label className="text-[12px] font-semibold text-[#6B7280]">
  Status
</label>

// Large numbers (KPIs) - very large, semibold
<div className="text-[36px] font-semibold text-[#F9FAFB]">
  98.7%
</div>
```

- [ ] **Step 3: Update Discovery page typography**

Apply same hierarchy to Discovery filters, container tables, and labels.

- [ ] **Step 4: Update Audit page typography**

Apply to audit logs list, timestamps, and event details.

- [ ] **Step 5: Verify in browser**

Run: `cd web && npm run dev`

Check pages and verify:
- Clear visual hierarchy
- Headings stand out
- Labels are muted but readable
- KPIs are prominent

Expected: Typography creates clear information hierarchy.

- [ ] **Step 6: Commit**

```bash
git add web/app/dashboard/page.tsx web/app/discovery/page.tsx web/app/audit/page.tsx
git commit -m "feat: enhance typography hierarchy across pages"
```

---

## Task 11: Refine Shadows and Depth

**Files:**
- Modify: Card components, elevated surfaces, focus states

- [ ] **Step 1: Update Card shadows**

Already done in Task 7, but verify in other components. Check any modals or drawers.

- [ ] **Step 2: Add focus ring shadows to interactive elements**

Update buttons and form inputs in `web/components/ui-modern/Button.tsx`:

```typescript
// Add focus styles
<button
  className={`
    ...existing styles...
    focus:outline-none
    focus:shadow-[0_0_0_3px_rgba(59,130,246,0.1),_0_0_0_1px_rgba(59,130,246,0.5)]
    transition-shadow duration-200
  `}
>
```

- [ ] **Step 3: Test shadow rendering**

Run: `cd web && npm run dev`

Check that:
- Cards have subtle depth
- Shadows are not harsh or neon
- Focus rings are visible but subtle

Expected: Shadows add depth without visual noise.

- [ ] **Step 4: Commit**

```bash
git add web/components/ui-modern/Button.tsx
git commit -m "feat: refine shadows for subtle depth"
```

---

## Task 12: Test Responsive Design Across Breakpoints

**Files:**
- Test files (manual testing via dev server)

- [ ] **Step 1: Test Desktop View (1920px)**

Run: `cd web && npm run dev`

Open browser at full width:
- Check Discovery page layout
- Check Dashboard cards
- Verify no horizontal scroll
- Check padding/margins

Expected: Content fits perfectly, no clipping.

- [ ] **Step 2: Test Tablet View (768px)**

Use browser DevTools, set device to iPad:
- Navigate to Discovery page
- Check filter panels
- Check table responsiveness
- Verify touch-friendly spacing

Expected: Layouts stack appropriately, spacing is adequate.

- [ ] **Step 3: Test Mobile View (375px)**

Use browser DevTools, set device to iPhone:
- Check all pages load
- Verify card widths
- Check text readability
- Ensure no horizontal scroll

Expected: Mobile layout is clean and usable.

- [ ] **Step 4: Document responsive status**

If any issues found, note them (don't fix in this task - log for follow-up).

- [ ] **Step 5: Commit responsive validation**

```bash
git commit --allow-empty -m "test: verify responsive design across desktop, tablet, mobile"
```

---

## Task 13: Verify WCAG AA Accessibility Compliance

**Files:**
- Audit components for accessibility

- [ ] **Step 1: Check color contrast**

Verify critical UI elements meet WCAG AA (4.5:1 for normal text, 3:1 for large):

- Text primary (#F9FAFB) on bg primary (#0B1020): ✓ ~19:1
- Text secondary (#9CA3AF) on bg secondary (#111827): ✓ ~5.5:1
- Status badge green text on green bg: Verify ~4.5:1

Expected: All combinations pass WCAG AA.

- [ ] **Step 2: Verify focus states**

Check that:
- Buttons have visible focus rings
- Form inputs show focus borders
- Tab order is logical

Run: `cd web && npm run dev`

Test using keyboard Tab navigation on Dashboard page.

Expected: Can navigate entire page with keyboard.

- [ ] **Step 3: Test with accessibility checker**

Use browser extension (axe DevTools):
- Run on Dashboard page
- Run on Discovery page
- Run on Audit page

Expected: No critical or serious issues.

- [ ] **Step 4: Commit accessibility verification**

```bash
git commit --allow-empty -m "test: verify WCAG AA accessibility compliance"
```

---

## Task 14: Final Integration Test and Visual Polish

**Files:**
- Integration testing (manual)

- [ ] **Step 1: Full app walkthrough**

Run: `cd web && npm run dev`

Navigate full app flow:
1. Login page - verify styling
2. Dashboard - check card styling, shadows, typography
3. Discovery page - verify filters, badges, tables
4. Audit page - check logs display
5. Settings/Profile pages - check consistency

Expected: Consistent Tech Noir look throughout.

- [ ] **Step 2: Check hover/active states**

Hover over cards, buttons, links:
- Verify smooth transitions
- Check color changes are subtle
- Ensure enough feedback

Expected: Interactive elements feel responsive.

- [ ] **Step 3: Dark background verification**

Verify primary background (#0B1020) is used on:
- Login page
- Main app pages
- Modals (where appropriate)

Expected: Cohesive dark aesthetic.

- [ ] **Step 4: Create summary commit**

```bash
git commit --allow-empty -m "feat: Tech Noir design system complete - 9.5/10 visual quality achieved"
```

---

## Spec Coverage Verification

✓ Centralized design tokens (colors.ts, spacing.ts, radius.ts, shadows.ts, typography.ts)
✓ Tech Noir color palette applied (#0B1020 backgrounds, #3B82F6 primary, etc.)
✓ Card styling refined with subtle depth and improved spacing
✓ Typography hierarchy enhanced (page titles, section headers, labels, KPIs)
✓ Status badges standardized (green/amber/red/blue)
✓ Shadows refined (subtle depth, no neon glows)
✓ Responsive design verified (desktop/tablet/mobile)
✓ WCAG AA accessibility maintained
✓ All functionality preserved (no layout changes)
✓ Visual quality target: 9.5/10

---

## Notes for Implementation

- **Color variables in CSS:** Use `--color-bg-primary`, `--color-text-primary`, etc. in Tailwind and components
- **Token imports:** Components can import from `@/lib/design-system` for programmatic access
- **Gradual rollout:** Can update components incrementally without breaking others
- **Testing strategy:** Visual testing via dev server + lighthouse for accessibility
- **No layout breaking:** All changes are styling-only, no HTML structure changes

