'use client'

export interface LoadingSkeletonProps {
  count?: number
  className?: string
  height?: 'sm' | 'md' | 'lg'
}

const heightMap = {
  sm: 'h-4',
  md: 'h-6',
  lg: 'h-8',
}

export function LoadingSkeleton({ count = 3, className = '', height = 'md' }: LoadingSkeletonProps) {
  return (
    <div className={`space-y-3 ${className}`}>
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className={`${heightMap[height]} bg-gray-200 rounded animate-pulse`} />
      ))}
    </div>
  )
}

export function LoadingSkeletonTable({ rows = 5 }: { rows?: number }) {
  return (
    <div className="space-y-3">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex gap-4">
          <div className="h-10 w-10 bg-gray-200 rounded animate-pulse" />
          <div className="flex-1 space-y-2">
            <div className="h-4 bg-gray-200 rounded animate-pulse w-3/4" />
            <div className="h-3 bg-gray-200 rounded animate-pulse w-1/2" />
          </div>
        </div>
      ))}
    </div>
  )
}

export function LoadingSkeletonGrid({ count = 4 }: { count?: number }) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="bg-gray-100 rounded-lg p-6">
          <div className="h-8 bg-gray-200 rounded animate-pulse mb-4" />
          <div className="h-6 bg-gray-200 rounded animate-pulse w-3/4" />
        </div>
      ))}
    </div>
  )
}
