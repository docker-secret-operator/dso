'use client'

import { Card, CardContent } from '@/components/ui/card'
import { ReactNode } from 'react'

export interface StatCardProps {
  title: string
  value: string | number
  subtitle?: string
  icon?: ReactNode
  trend?: {
    value: number
    direction: 'up' | 'down'
  }
  color?: 'green' | 'blue' | 'yellow' | 'red'
  loading?: boolean
}

export function StatCard({
  title,
  value,
  subtitle,
  icon,
  trend,
  color = 'blue',
  loading = false,
}: StatCardProps) {
  const colorMap = {
    green: 'bg-green-50 border-green-200',
    blue: 'bg-blue-50 border-blue-200',
    yellow: 'bg-yellow-50 border-yellow-200',
    red: 'bg-red-50 border-red-200',
  }

  const textColorMap = {
    green: 'text-green-700',
    blue: 'text-blue-700',
    yellow: 'text-yellow-700',
    red: 'text-red-700',
  }

  const trendColorMap = {
    up: 'text-green-600',
    down: 'text-red-600',
  }

  return (
    <Card className={colorMap[color]}>
      <CardContent className="pt-6">
        <div className="flex items-center justify-between">
          <div className="flex-1">
            <p className="text-sm font-medium text-gray-600">{title}</p>
            <div className="flex items-baseline gap-2 mt-2">
              {loading ? (
                <div className="h-8 w-20 bg-gray-200 rounded animate-pulse" />
              ) : (
                <>
                  <p className={`text-3xl font-bold ${textColorMap[color]}`}>{value}</p>
                  {trend && (
                    <span className={`text-sm font-semibold ${trendColorMap[trend.direction]}`}>
                      {trend.direction === 'up' ? '↑' : '↓'} {Math.abs(trend.value)}%
                    </span>
                  )}
                </>
              )}
            </div>
            {subtitle && <p className="text-xs text-gray-500 mt-1">{subtitle}</p>}
          </div>
          {icon && <div className="ml-4 text-gray-400">{icon}</div>}
        </div>
      </CardContent>
    </Card>
  )
}
