'use client'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ErrorBoundary } from '@/components/error-boundary'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/lib/api-client'
import { formatTime } from '@/lib/utils'
import { Loader2, Download } from 'lucide-react'

export default function AuditPage() {
  const { data: logs = [], isLoading } = useQuery({
    queryKey: ['logs'],
    queryFn: () => apiClient.getLogs(undefined, 100),
    refetchInterval: 30000,
  })

  const handleDownloadCSV = () => {
    const csv = [
      ['Timestamp', 'Level', 'Message', 'Secret', 'Provider'].join(','),
      ...logs.map((log) =>
        [
          log.timestamp,
          log.level,
          `"${log.message}"`,
          log.secret_name || '',
          log.provider || '',
        ].join(',')
      ),
    ].join('\n')

    const blob = new Blob([csv], { type: 'text/csv' })
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `dso-audit-${new Date().toISOString().split('T')[0]}.csv`
    a.click()
    window.URL.revokeObjectURL(url)
  }

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'error':
        return 'destructive'
      case 'warning':
        return 'secondary'
      default:
        return 'default'
    }
  }

  return (
    <ErrorBoundary>
      <div className="space-y-8 p-8">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold text-foreground">Audit Log</h1>
          <p className="text-muted-foreground">All system activities and operations</p>
        </div>
        <Button onClick={handleDownloadCSV} variant="outline">
          <Download className="mr-2 h-4 w-4" />
          Export CSV
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Activity Log</CardTitle>
          <CardDescription>Latest {logs.length} log entries</CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : logs.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              <p>No log entries</p>
            </div>
          ) : (
            <div className="space-y-3">
              {logs.map((log, idx) => (
                <div
                  key={idx}
                  className="flex items-start gap-4 border-b border-border pb-3 last:border-0"
                >
                  <div className="mt-0.5">
                    <Badge variant={getLevelColor(log.level)}>
                      {log.level.toUpperCase()}
                    </Badge>
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-foreground">
                      {log.message}
                    </p>
                    <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                      <span>{formatTime(log.timestamp)}</span>
                      {log.secret_name && (
                        <span>Secret: <span className="font-mono">{log.secret_name}</span></span>
                      )}
                      {log.provider && (
                        <span>Provider: <span className="capitalize">{log.provider}</span></span>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
      </div>
    </ErrorBoundary>
  )
}
