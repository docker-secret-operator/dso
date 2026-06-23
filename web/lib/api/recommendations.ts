export interface Recommendation {
  id: string
  title: string
  description: string
  reason?: string
  resource?: string
  priority: 'critical' | 'high' | 'medium' | 'low'
  category: 'rotation' | 'drift' | 'compliance' | 'policy' | 'operational' | string
  status: 'open' | 'acknowledged' | 'implemented' | 'dismissed'
  resource_id?: string
  suggested_action: string
  confidence: number
  driftId?: string
  policyId?: string
  auditId?: string
  created_at: number
}

export interface RecommendationListResponse {
  recommendations: Recommendation[]
  count: number
  total: number
  page: number
  pageSize: number
}

export interface RecommendationMetrics {
  total_recommendations: number
  open_recommendations: number
  acknowledged_recommendations: number
  implemented_recommendations: number
  dismissed_recommendations: number
  average_confidence: number
  last_updated: string
}

export async function listRecommendations(params?: {
  severity?: string
  category?: string
  page?: number
  pageSize?: number
}): Promise<RecommendationListResponse> {
  const sp = new URLSearchParams()
  if (params?.severity) sp.set('severity', params.severity)
  if (params?.category) sp.set('category', params.category)
  if (params?.page) sp.set('page', String(params.page))
  if (params?.pageSize) sp.set('pageSize', String(params.pageSize))
  const qs = sp.toString()
  const res = await fetch(`/api/recommendations${qs ? `?${qs}` : ''}`, { credentials: 'include' })
  if (!res.ok) throw new Error(`Failed to load recommendations: ${res.statusText}`)
  return res.json()
}

export async function getRecommendationMetrics(): Promise<RecommendationMetrics> {
  const res = await fetch('/api/recommendations/metrics', { credentials: 'include' })
  if (!res.ok) throw new Error(`Failed to load recommendation metrics: ${res.statusText}`)
  return res.json()
}

export async function acknowledgeRecommendation(id: string): Promise<void> {
  const res = await fetch(`/api/recommendations/${encodeURIComponent(id)}/acknowledge`, {
    method: 'POST',
    credentials: 'include',
  })
  if (!res.ok) throw new Error(`Failed to acknowledge recommendation: ${res.statusText}`)
}

export async function dismissRecommendation(id: string): Promise<void> {
  const res = await fetch(`/api/recommendations/${encodeURIComponent(id)}/dismiss`, {
    method: 'POST',
    credentials: 'include',
  })
  if (!res.ok) throw new Error(`Failed to dismiss recommendation: ${res.statusText}`)
}
