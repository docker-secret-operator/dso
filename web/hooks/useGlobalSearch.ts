import { useMemo, useCallback, useState, useEffect } from 'react'

export interface SearchResult {
  id: string
  title: string
  subtitle?: string
  category: 'container' | 'secret' | 'event' | 'correlation' | 'execution' | 'actor'
  icon?: string
  route: string
  metadata?: Record<string, string>
}

export interface UseGlobalSearchProps {
  isOpen: boolean
  onOpenChange: (open: boolean) => void
  containers?: any[]
  secrets?: any[]
  events?: any[]
  auditEvents?: any[]
}

export function useGlobalSearch({ isOpen, onOpenChange, containers = [], secrets = [], events = [], auditEvents = [] }: UseGlobalSearchProps) {
  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')

  useEffect(() => {
    const t = setTimeout(() => setDebouncedQuery(query), 300)
    return () => clearTimeout(t)
  }, [query])

  // Memoize search index to avoid recalculating on every render
  const searchIndex = useMemo(() => {
    const results: SearchResult[] = []
    const seenCorrelations = new Set<string>()
    const seenExecutions = new Set<string>()
    const seenActors = new Set<string>()

    // Index audit events — deduplicate by correlation_id, execution_id, actor
    auditEvents.forEach((e: any) => {
      if (e.correlation_id && !seenCorrelations.has(e.correlation_id)) {
        seenCorrelations.add(e.correlation_id)
        results.push({
          id: `corr-${e.correlation_id}`,
          title: e.correlation_id,
          subtitle: `Correlation chain — ${e.action}`,
          category: 'correlation',
          route: `/audit?correlation_id=${encodeURIComponent(e.correlation_id)}`,
          metadata: { action: e.action, actor: e.actor },
        })
      }
      if (e.resource_id && e.resource_type === 'execution' && !seenExecutions.has(e.resource_id)) {
        seenExecutions.add(e.resource_id)
        results.push({
          id: `exec-${e.resource_id}`,
          title: e.resource_id,
          subtitle: `Execution journey — ${e.actor}`,
          category: 'execution',
          route: `/executions?id=${encodeURIComponent(e.resource_id)}`,
          metadata: { actor: e.actor, status: e.status },
        })
      }
      if (e.actor_id && e.actor && !seenActors.has(e.actor_id)) {
        seenActors.add(e.actor_id)
        results.push({
          id: `actor-${e.actor_id}`,
          title: e.actor,
          subtitle: `Actor timeline — ${e.actor_id}`,
          category: 'actor',
          route: `/users/activity?id=${encodeURIComponent(e.actor_id)}`,
          metadata: { actor_id: e.actor_id },
        })
      }
    })

    // Index containers
    containers.forEach((container: any) => {
      results.push({
        id: `container-${container.id}`,
        title: container.name,
        subtitle: container.image?.substring(0, 50),
        category: 'container',
        route: `/discovery?container=${container.id}`,
        metadata: {
          status: container.status,
          image: container.image,
        },
      })
    })

    // Index secrets
    secrets.forEach((secret: any) => {
      results.push({
        id: `secret-${secret.name}`,
        title: secret.name,
        subtitle: `Provider: ${secret.provider}`,
        category: 'secret',
        route: `/secrets?secret=${secret.name}`,
        metadata: {
          provider: secret.provider,
          status: secret.status,
        },
      })
    })

    // Index events (last 50)
    events.slice(0, 50).forEach((event: any, idx: number) => {
      results.push({
        id: `event-${idx}`,
        title: event.message || event.action || 'Event',
        subtitle: event.secret_name ? `Secret: ${event.secret_name}` : undefined,
        category: 'event',
        route: `/timeline?event=${idx}`,
        metadata: {
          severity: event.severity,
          timestamp: event.timestamp,
        },
      })
    })

    return results
  }, [containers, secrets, events])

  // Memoize search filtering to avoid expensive operations
  const results = useMemo(() => {
    if (!debouncedQuery.trim()) {
      return []
    }

    const q = debouncedQuery.toLowerCase()
    return searchIndex
      .filter(
        (result) =>
          result.title.toLowerCase().includes(q) ||
          result.subtitle?.toLowerCase().includes(q) ||
          Object.values(result.metadata || {}).some((v) => String(v).toLowerCase().includes(q))
      )
      .slice(0, 50)
  }, [debouncedQuery, searchIndex])

  // Group results by category
  const groupedResults = useMemo(() => {
    const grouped = {
      container: [] as SearchResult[],
      secret: [] as SearchResult[],
      event: [] as SearchResult[],
      correlation: [] as SearchResult[],
      execution: [] as SearchResult[],
      actor: [] as SearchResult[],
    }

    results.forEach((result) => {
      grouped[result.category].push(result)
    })

    return grouped
  }, [results])

  const handleQueryChange = useCallback((value: string) => {
    setQuery(value)
  }, [])

  const handleOpenChange = useCallback((open: boolean) => {
    onOpenChange(open)
  }, [onOpenChange])

  const handleNavigate = useCallback((route: string) => {
    setQuery('')
    onOpenChange(false)
    return route
  }, [onOpenChange])

  return {
    query,
    isOpen,
    results,
    groupedResults,
    totalResults: results.length,
    handleQueryChange,
    handleOpenChange,
    handleNavigate,
    hasResults: results.length > 0,
    isEmpty: debouncedQuery.trim().length > 0 && results.length === 0,
  }
}
