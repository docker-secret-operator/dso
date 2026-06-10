'use client'

import { useState, useEffect } from 'react'
import { TrendingUp, AlertTriangle, Zap, Network, Lightbulb } from 'lucide-react'

interface Node {
  id: string
  type: string
  name: string
}

interface GraphMetrics {
  total_nodes: number
  total_edges: number
  average_degree: number
  max_fan_in: number
  max_fan_out: number
  max_depth: number
  average_path_length: number
  cycles: number
  critical_nodes: number
  connected_components: number
}

interface Component {
  id: number
  nodes: Node[]
  size: number
}

export default function GraphPage() {
  const [nodes, setNodes] = useState<Node[]>([])
  const [metrics, setMetrics] = useState<GraphMetrics | null>(null)
  const [components, setComponents] = useState<Component[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [overviewRes, metricsRes, componentsRes] = await Promise.all([
          fetch('/api/graph'),
          fetch('/api/graph/metrics'),
          fetch('/api/graph/components'),
        ])

        if (!overviewRes.ok || !metricsRes.ok) {
          throw new Error('Failed to fetch graph data')
        }

        const overviewData = await overviewRes.json()
        const metricsData = await metricsRes.json()
        const componentsData = componentsRes.ok ? await componentsRes.json() : { components: [] }

        setNodes(overviewData.nodes || [])
        setMetrics(metricsData)
        setComponents(componentsData.components || [])
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 30000)
    return () => clearInterval(interval)
  }, [])

  if (loading && !metrics) {
    return <div className="p-8">Loading...</div>
  }

  const typeColors: Record<string, string> = {
    secret: 'bg-purple-100 text-purple-800',
    policy: 'bg-blue-100 text-blue-800',
    plugin: 'bg-indigo-100 text-indigo-800',
    integration: 'bg-green-100 text-green-800',
    user: 'bg-red-100 text-red-800',
    session: 'bg-yellow-100 text-yellow-800',
    scheduler_job: 'bg-cyan-100 text-cyan-800',
    alert: 'bg-orange-100 text-orange-800',
    backup: 'bg-pink-100 text-pink-800',
    execution: 'bg-gray-100 text-gray-800',
    review: 'bg-emerald-100 text-emerald-800',
    approval: 'bg-violet-100 text-violet-800',
    drift: 'bg-red-100 text-red-800',
    metric: 'bg-teal-100 text-teal-800',
    security: 'bg-rose-100 text-rose-800',
    notification: 'bg-amber-100 text-amber-800',
  }

  return (
    <div className="space-y-8 p-8">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">Dependency Graph</h1>
        <p className="mt-2 text-gray-600">Visualize relationships and analyze impact of changes</p>
      </div>

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800">
          {error}
        </div>
      )}

      {/* Metrics Summary - Extended */}
      {metrics && (
        <>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-6">
            <MetricCard label="Nodes" value={metrics.total_nodes} icon={<TrendingUp className="h-5 w-5" />} />
            <MetricCard label="Edges" value={metrics.total_edges} />
            <MetricCard
              label="Avg Degree"
              value={metrics.average_degree.toFixed(2)}
              valueClass="text-blue-600"
            />
            <MetricCard
              label="Cycles"
              value={metrics.cycles}
              valueClass={metrics.cycles > 0 ? 'text-red-600' : 'text-green-600'}
              icon={metrics.cycles > 0 ? <AlertTriangle className="h-5 w-5" /> : undefined}
            />
            <MetricCard
              label="Critical"
              value={metrics.critical_nodes}
              valueClass="text-orange-600"
              icon={<Zap className="h-5 w-5" />}
            />
            <MetricCard
              label="Components"
              value={metrics.connected_components}
              icon={<Network className="h-5 w-5" />}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
            <MetricCard label="Max Fan-In" value={metrics.max_fan_in} />
            <MetricCard label="Max Fan-Out" value={metrics.max_fan_out} />
            <MetricCard label="Max Depth" value={metrics.max_depth} />
            <MetricCard
              label="Avg Path Length"
              value={metrics.average_path_length.toFixed(2)}
              valueClass="text-indigo-600"
            />
          </div>
        </>
      )}

      {/* Connected Components */}
      {components.length > 0 && (
        <div className="rounded-lg border border-gray-200 bg-white">
          <div className="border-b border-gray-200 px-6 py-4">
            <div className="flex items-center gap-2">
              <Network className="h-5 w-5 text-gray-600" />
              <h2 className="font-semibold text-gray-900">Connected Components ({components.length})</h2>
            </div>
          </div>

          <div className="divide-y divide-gray-200">
            {components.map(comp => (
              <div key={comp.id} className="px-6 py-4">
                <div className="flex items-center justify-between">
                  <div>
                    <p className="font-medium text-gray-900">Component {comp.id + 1}</p>
                    <p className="text-sm text-gray-600">{comp.size} nodes</p>
                  </div>
                  <div className="text-right">
                    <div className="flex gap-1 flex-wrap justify-end">
                      {comp.nodes.slice(0, 5).map(node => (
                        <span
                          key={node.id}
                          className={`rounded px-2 py-1 text-xs font-medium ${
                            typeColors[node.type] || 'bg-gray-100 text-gray-800'
                          }`}
                        >
                          {node.type}
                        </span>
                      ))}
                      {comp.size > 5 && <span className="text-xs text-gray-500">+{comp.size - 5}</span>}
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Nodes Table */}
      <div className="rounded-lg border border-gray-200 bg-white">
        <div className="border-b border-gray-200 px-6 py-4">
          <h2 className="font-semibold text-gray-900">Nodes ({nodes.length})</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-gray-200 bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Name</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Type</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">ID</th>
              </tr>
            </thead>
            <tbody>
              {nodes.length === 0 ? (
                <tr>
                  <td colSpan={3} className="px-6 py-8 text-center text-gray-500">
                    No nodes in graph
                  </td>
                </tr>
              ) : (
                nodes.slice(0, 50).map(node => (
                  <tr key={node.id} className="border-b border-gray-200 hover:bg-gray-50">
                    <td className="px-6 py-4 font-medium text-gray-900">{node.name}</td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded px-2 py-1 text-xs font-medium ${
                          typeColors[node.type] || 'bg-gray-100 text-gray-800'
                        }`}
                      >
                        {node.type}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-600">{node.id}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {nodes.length > 50 && (
          <div className="border-t border-gray-200 px-6 py-4 text-sm text-gray-600">
            Showing 50 of {nodes.length} nodes
          </div>
        )}
      </div>

      {/* Graph Info */}
      <div className="rounded-lg border border-gray-200 bg-white p-6">
        <div className="flex items-center gap-2 mb-4">
          <Lightbulb className="h-5 w-5 text-gray-600" />
          <h3 className="font-semibold text-gray-900">Graph Information</h3>
        </div>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4 text-sm">
          <div>
            <span className="text-gray-600">Node Types</span>
            <div className="font-medium text-gray-900">
              {new Set(nodes.map(n => n.type)).size}
            </div>
          </div>
          <div>
            <span className="text-gray-600">Max Degree</span>
            <div className="font-medium text-gray-900">
              {metrics && metrics.total_nodes > 0 ? '~' + Math.ceil(metrics.average_degree * 2) : '0'}
            </div>
          </div>
          <div>
            <span className="text-gray-600">Density</span>
            <div className="font-medium text-gray-900">
              {metrics && metrics.total_nodes > 1
                ? ((metrics.total_edges / (metrics.total_nodes * (metrics.total_nodes - 1))) * 100).toFixed(2)
                : '0'}
              %
            </div>
          </div>
          <div>
            <span className="text-gray-600">Graph Health</span>
            <div className={`font-medium ${metrics && metrics.cycles === 0 ? 'text-green-600' : 'text-red-600'}`}>
              {metrics && metrics.cycles === 0 ? 'Healthy' : 'Has Cycles'}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

interface MetricCardProps {
  label: string
  value: string | number
  icon?: React.ReactNode
  valueClass?: string
}

function MetricCard({ label, value, icon, valueClass = 'text-gray-900' }: MetricCardProps) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <div className="flex items-center justify-between">
        <span className="text-sm text-gray-600">{label}</span>
        {icon && <div className="text-gray-400">{icon}</div>}
      </div>
      <div className={`mt-2 text-2xl font-bold ${valueClass}`}>{value}</div>
    </div>
  )
}
