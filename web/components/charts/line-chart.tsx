'use client'

import { useMemo } from 'react'

interface DataPoint {
  x: number  // timestamp (Unix seconds)
  y: number
}

interface LineChartProps {
  data: DataPoint[]
  label?: string
  color?: string
  height?: number
  yMin?: number
  yMax?: number
  formatY?: (v: number) => string
  formatX?: (ts: number) => string
  className?: string
}

export function LineChart({
  data,
  label,
  color = '#3b82f6',
  height = 80,
  yMin,
  yMax,
  formatY = (v) => v.toFixed(2),
  className = '',
}: LineChartProps) {
  const width = 300

  const { path, dots, minVal, maxVal } = useMemo(() => {
    if (data.length === 0) return { path: '', dots: [], minVal: 0, maxVal: 1 }

    const ys = data.map(d => d.y)
    const xs = data.map(d => d.x)
    const minY = yMin ?? Math.min(...ys)
    const maxY = yMax ?? Math.max(...ys)
    const rangeY = maxY - minY || 1
    const minX = Math.min(...xs)
    const maxX = Math.max(...xs)
    const rangeX = maxX - minX || 1
    const pad = 4

    const toSvg = (d: DataPoint) => ({
      cx: pad + ((d.x - minX) / rangeX) * (width - 2 * pad),
      cy: pad + (1 - (d.y - minY) / rangeY) * (height - 2 * pad),
    })

    const pts = data.map(toSvg)
    const pathStr = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.cx.toFixed(1)},${p.cy.toFixed(1)}`).join(' ')

    // area fill path
    const first = pts[0]
    const last = pts[pts.length - 1]
    const areaPath = pathStr +
      ` L${last.cx.toFixed(1)},${(height - pad).toFixed(1)}` +
      ` L${first.cx.toFixed(1)},${(height - pad).toFixed(1)} Z`

    return { path: pathStr, areaPath, dots: pts, minVal: minY, maxVal: maxY }
  }, [data, height, yMin, yMax])

  if (data.length === 0) {
    return (
      <div className={`flex items-center justify-center text-xs text-muted-foreground h-[${height}px] ${className}`}>
        No data
      </div>
    )
  }

  const lastVal = data[data.length - 1]?.y ?? 0

  return (
    <div className={`space-y-1 ${className}`}>
      {label && (
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted-foreground">{label}</span>
          <span className="text-xs font-semibold tabular-nums">{formatY(lastVal)}</span>
        </div>
      )}
      <svg
        width="100%"
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        className="overflow-visible"
        style={{ height }}
      >
        <defs>
          <linearGradient id={`grad-${label}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.2" />
            <stop offset="100%" stopColor={color} stopOpacity="0.02" />
          </linearGradient>
        </defs>
        {/* Area fill */}
        <path
          d={`${path} L${(dots[dots.length - 1]?.cx ?? 0).toFixed(1)},${(height - 4).toFixed(1)} L${(dots[0]?.cx ?? 0).toFixed(1)},${(height - 4).toFixed(1)} Z`}
          fill={`url(#grad-${label})`}
          stroke="none"
        />
        {/* Line */}
        <path d={path} fill="none" stroke={color} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <div className="flex justify-between text-[10px] text-muted-foreground tabular-nums">
        <span>{formatY(minVal)}</span>
        <span>{formatY(maxVal)}</span>
      </div>
    </div>
  )
}
