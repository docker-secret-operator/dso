'use client'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AlertCircle, AlertTriangle, CheckCircle } from 'lucide-react'
import Link from 'next/link'

interface ImpactAnalysisProps {
  resourceName: string
  resourceType: 'secret' | 'container'
  affectedCount: number
  recentIssues: number
  severity: 'high' | 'medium' | 'low'
  affectedItems?: Array<{
    name: string
    status: string
  }>
}

export function ImpactAnalysisCard({
  resourceName,
  resourceType,
  affectedCount,
  recentIssues,
  severity,
  affectedItems = [],
}: ImpactAnalysisProps) {
  const severityConfig = {
    high: {
      icon: AlertCircle,
      bg: 'bg-red-50',
      border: 'border-red-200',
      badge: 'bg-red-100 text-red-800',
      text: 'text-red-900',
    },
    medium: {
      icon: AlertTriangle,
      bg: 'bg-yellow-50',
      border: 'border-yellow-200',
      badge: 'bg-yellow-100 text-yellow-800',
      text: 'text-yellow-900',
    },
    low: {
      icon: CheckCircle,
      bg: 'bg-green-50',
      border: 'border-green-200',
      badge: 'bg-green-100 text-green-800',
      text: 'text-green-900',
    },
  }

  const config = severityConfig[severity]
  const Icon = config.icon

  const potentialImpactText = {
    high: 'Critical - Immediate attention required. Multiple containers or recent errors.',
    medium: 'Moderate - Monitor closely. May affect operations.',
    low: 'Low - Minimal risk. Routine monitoring sufficient.',
  }

  return (
    <Card className={`${config.bg} border-2 ${config.border}`}>
      <CardHeader>
        <div className="flex items-start justify-between gap-4">
          <div className="flex items-start gap-3">
            <Icon className={`w-6 h-6 ${config.text} flex-shrink-0 mt-1`} />
            <div>
              <CardTitle className={config.text}>{resourceName}</CardTitle>
              <CardDescription className={config.text}>
                {resourceType === 'secret' ? 'Secret Impact Analysis' : 'Container Impact Analysis'}
              </CardDescription>
            </div>
          </div>
          <Badge className={config.badge}>
            {severity === 'high' ? 'Critical' : severity === 'medium' ? 'Warning' : 'Healthy'}
          </Badge>
        </div>
      </CardHeader>

      <CardContent className="space-y-6">
        {/* Impact Metrics */}
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-xs uppercase font-semibold text-gray-600 mb-2">
              Affected {resourceType === 'secret' ? 'Containers' : 'Secrets'}
            </p>
            <p className={`text-3xl font-bold ${config.text}`}>{affectedCount}</p>
          </div>
          <div>
            <p className="text-xs uppercase font-semibold text-gray-600 mb-2">Recent Issues</p>
            <p className={`text-3xl font-bold ${config.text}`}>{recentIssues}</p>
          </div>
        </div>

        {/* Affected Items */}
        {affectedItems.length > 0 && (
          <div className="pt-4 border-t">
            <p className="text-xs uppercase font-semibold text-gray-700 mb-3">
              Affected {resourceType === 'secret' ? 'Containers' : 'Secrets'}
            </p>
            <div className="space-y-2 max-h-48 overflow-y-auto">
              {affectedItems.slice(0, 5).map((item, idx) => (
                <div
                  key={idx}
                  className="flex items-center justify-between p-2 bg-white rounded border"
                >
                  <span className="text-sm font-medium text-gray-900">{item.name}</span>
                  <Badge
                    variant={item.status === 'error' ? 'destructive' : 'outline'}
                    className="text-xs"
                  >
                    {item.status}
                  </Badge>
                </div>
              ))}
              {affectedItems.length > 5 && (
                <p className="text-xs text-gray-600 italic">
                  +{affectedItems.length - 5} more {resourceType === 'secret' ? 'containers' : 'secrets'}
                </p>
              )}
            </div>
          </div>
        )}

        {/* Potential Impact */}
        <div className="pt-4 border-t">
          <p className="text-xs uppercase font-semibold text-gray-700 mb-2">Potential Impact</p>
          <p className={`text-sm ${config.text}`}>{potentialImpactText[severity]}</p>
        </div>

        {/* Action Link */}
        <div className="pt-4 border-t">
          <Link
            href={
              resourceType === 'secret'
                ? `/secrets?name=${encodeURIComponent(resourceName)}`
                : `/discovery?container=${encodeURIComponent(resourceName)}`
            }
            className={`inline-flex text-sm font-medium ${config.text} hover:underline`}
          >
            View details →
          </Link>
        </div>
      </CardContent>
    </Card>
  )
}
