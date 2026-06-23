export type ForecastCategory = 'rotation' | 'drift' | 'compliance' | 'operational'
export type ForecastSeverity = 'critical' | 'high' | 'medium' | 'low' | 'info'

/**
 * P9 operational forecast — statistical, evidence-based, never AI-generated.
 * The `beta` field will always be true for operational forecasts.
 */
export interface OperationalForecast {
  id: string
  category: ForecastCategory
  severity: ForecastSeverity
  title: string
  description: string
  reason: string
  resource?: string
  confidence: number
  predicted_at: number
  evidence: string[]
  beta: true
}

export interface ForecastListResponse {
  forecasts: OperationalForecast[]
  count: number
  total: number
  page: number
  pageSize: number
  beta: boolean
}

export async function listForecasts(params?: {
  category?: ForecastCategory
  severity?: ForecastSeverity
  page?: number
  pageSize?: number
}): Promise<ForecastListResponse> {
  const sp = new URLSearchParams()
  if (params?.category) sp.set('category', params.category)
  if (params?.severity) sp.set('severity', params.severity)
  if (params?.page) sp.set('page', String(params.page))
  if (params?.pageSize) sp.set('pageSize', String(params.pageSize))
  const qs = sp.toString()
  const res = await fetch(`/api/forecasts${qs ? `?${qs}` : ''}`, { credentials: 'include' })
  if (!res.ok) throw new Error(`Failed to load forecasts: ${res.statusText}`)
  return res.json()
}
