'use client'

import Image from 'next/image'
import Link from 'next/link'
import { cn } from '@/lib/utils'

interface LogoProps {
  href?: string
  className?: string
  showText?: boolean
  size?: 'sm' | 'md' | 'lg'
}

export function Logo({
  href = '/',
  className = '',
  showText = true,
  size = 'md',
}: LogoProps) {
  const sizeMap = {
    sm: 'h-8',
    md: 'h-10',
    lg: 'h-12',
  }

  const content = (
    <div className={cn('flex items-center gap-2 group', className)}>
      <div className={cn(
        'relative transition-transform duration-300 group-hover:scale-110',
        sizeMap[size]
      )}>
        <Image
          src="/dso-logo-navbar.svg"
          alt="DSO Logo"
          width={40}
          height={40}
          className="w-auto h-full"
          priority
        />
      </div>
      {showText && (
        <span className="text-[14px] font-semibold text-[#F9FAFB] tracking-tight hidden sm:inline">
          DSO
        </span>
      )}
    </div>
  )

  if (href) {
    return (
      <Link href={href}>
        {content}
      </Link>
    )
  }

  return content
}
