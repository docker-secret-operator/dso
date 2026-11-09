// Flat ESLint config (ESLint 9). Non-interactive and deterministic: `npm run lint`
// never prompts, and CI fails on any warning.
import { dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { FlatCompat } from '@eslint/eslintrc'

const compat = new FlatCompat({ baseDirectory: dirname(fileURLToPath(import.meta.url)) })

const config = [
  {
    ignores: ['node_modules/**', 'out/**', '.next/**', 'coverage/**', 'next-env.d.ts', 'tests/e2e/**'],
  },
  // eslint-config-next bundles @typescript-eslint, react, react-hooks, jsx-a11y
  // and the Next.js rules (core-web-vitals = the stricter set).
  ...compat.extends('next/core-web-vitals', 'next/typescript'),
  {
    // Tailwind's config is CommonJS; its plugin list uses require(). Scoped to
    // this one file and one rule rather than disabling the rule globally.
    files: ['tailwind.config.js', 'postcss.config.js'],
    rules: { '@typescript-eslint/no-require-imports': 'off' },
  },
]

export default config
