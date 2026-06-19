import { Card, MetricCard, Skeleton } from '@/components/ui-modern'
import { TrendingUp, TrendingDown, Zap, Cpu, Activity } from 'lucide-react'
import type { OperationsDashboard } from '@/lib/api/types'

interface OperationsOverviewProps {
  data?: OperationsDashboard
  isLoading?: boolean
  error?: string | null
}

/**
 * Operations overview displaying 5 KPI cards
 * Success rate, Failure rate, Throughput, Worker utilization, Total executions
 */
export function OperationsOverview({ data, isLoading, error }: OperationsOverviewProps) {
  if (isLoading) {
    return (
      <div className="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-5 gap-4">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="rounded-xl border border-white/[0.07] bg-[#111318] p-5 space-y-3">
            <Skeleton className="h-3.5 w-20 rounded" />
            <Skeleton className="h-8 w-16 rounded" />
            <Skeleton className="h-3 w-24 rounded" />
          </div>
        ))}
      </div>
    )
  }

  if (error) {
    return (
      <Card className="p-6">
        <p className="text-red-400 text-sm">{error}</p>
      </Card>
    )
  }

  if (!data?.overview_kpis) {
    return (
      <Card className="p-6">
        <p className="text-slate-400 text-sm">No operations data available</p>
      </Card>
    )
  }

  const kpis = data.overview_kpis

  // Format numbers with proper precision
  const successRate = kpis.success_rate ?? 0
  const failureRate = kpis.failure_rate ?? 0
  const throughput = kpis.throughput_per_second ?? 0
  const workerUtil = kpis.worker_utilization ?? 0
  const totalExecutions = kpis.totals?.executed ?? 0

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-5 gap-4">
      <MetricCard
        label="Success Rate"
        value={`${successRate.toFixed(1)}%`}
        sublabel="of executions"
        icon={<TrendingUp className="w-4 h-4" />}
        accentColor="emerald"
      />
      <MetricCard
        label="Failure Rate"
        value={`${failureRate.toFixed(1)}%`}
        sublabel="of executions"
        icon={<TrendingDown className="w-4 h-4" />}
        accentColor={failureRate > 10 ? 'red' : 'slate'}
      />
      <MetricCard
        label="Throughput"
        value={`${throughput.toFixed(2)}/sec`}
        sublabel="execution rate"
        icon={<Zap className="w-4 h-4" />}
        accentColor="blue"
      />
      <MetricCard
        label="Worker Util"
        value={`${workerUtil.toFixed(0)}%`}
        sublabel="average"
        icon={<Cpu className="w-4 h-4" />}
        accentColor={workerUtil > 85 ? 'red' : 'blue'}
      />
      <MetricCard
        label="Total Executions"
        value={totalExecutions.toLocaleString()}
        sublabel="all-time"
        icon={<Activity className="w-4 h-4" />}
        accentColor="indigo"
      />
    </div>
  )
}
