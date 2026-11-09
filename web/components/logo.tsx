import * as React from 'react'

// DSO brand mark: a padlock (secret custody) framed by container brackets
// (Docker/orchestration scope) -- the two things the product name names.
// Monoline, currentColor throughout so it inherits text color from its
// container and stays legible at favicon size (16px).
export function Logo(props: React.SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" {...props}>
      {/* container brackets */}
      <path d="M6 3H4.5A1.5 1.5 0 0 0 3 4.5V19.5A1.5 1.5 0 0 0 4.5 21H6" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M18 3h1.5A1.5 1.5 0 0 1 21 4.5V19.5A1.5 1.5 0 0 1 19.5 21H18" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
      {/* padlock */}
      <rect x="8.25" y="11" width="7.5" height="6" rx="1.25" stroke="currentColor" strokeWidth="1.75" />
      <path d="M9.75 11V9a2.25 2.25 0 0 1 4.5 0v2" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" />
      <circle cx="12" cy="13.85" r="1" fill="currentColor" />
    </svg>
  )
}
