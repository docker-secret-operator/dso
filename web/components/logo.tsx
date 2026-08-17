import * as React from 'react'

// DSO brand mark: rotating-arrow with a checkpoint marker, inlined from the
// marketing site's public/logo.svg (docker-secret-operator repo). Uses
// currentColor throughout so it inherits text color from its container.
export function Logo(props: React.SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" {...props}>
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      <path d="M 12 3 L 14 6 L 10 6 Z" fill="currentColor" />
      <rect x="10.5" y="1.5" width="3" height="3" stroke="currentColor" strokeWidth="2" fill="none" rx="0.5" />
    </svg>
  )
}
