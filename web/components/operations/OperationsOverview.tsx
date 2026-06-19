import { Card, MetricCard } from '@/components/ui-modern'
import { TrendingUp, TrendingDown, Zap, Cpu } from 'lucide-react'
import type { OperationsDashboard } from '@/lib/api/types'

interface OperationsOverviewProps {
  data?: OperationsDashboard
}

/**
 * Operations overview displaying KPI cards
 */
export function OperationsOverview({ data }: OperationsOverviewProps) {
  if (!data?.overview_kpis) {
    return (
      <Card className="p-6">
        <p className="text-slate-400 text-sm">No operations data available</p>
      </Card>
    )
  }

  const kpis = data.overview_kpis

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
      <MetricCard
        label="Success Rate"
        value={`${Math.round(kpis.success_rate)}%`}
        sublabel="executions"
        icon={<TrendingUp className="w-4 h-4" />}
        accentColor="emerald"
      />
      <MetricCard
        label="Failure Rate"
        value={`${Math.round(kpis.failure_rate)}%`}
        sublabel="executions"
        icon={<TrendingDown className="w-4 h-4" />}
        accentColor={kpis.failure_rate > 10 ? 'red' : 'slate'}
      />
      <MetricCard
        label="Throughput"
        value={`${(kpis.throughput_per_second ?? 0).toFixed(1)}`}
        sublabel="executions/sec"
        icon={<Zap className="w-4 h-4" />}
        accentColor="blue"
      />
      <MetricCard
        label="Worker Util"
        value={`${Math.round(kpis.worker_utilization)}%`}
        sublabel="average"
        icon={<Cpu className="w-4 h-4" />}
        accentColor={kpis.worker_utilization > 85 ? 'red' : 'blue'}
      />
    </div>
  )
}
