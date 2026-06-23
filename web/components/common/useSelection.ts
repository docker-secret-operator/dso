import { useState, useCallback } from 'react'

export interface UseSelection {
  selected: Set<string>
  toggle: (id: string) => void
  togglePage: (ids: string[]) => void
  clear: () => void
  isSelected: (id: string) => boolean
  size: number
}

export function useSelection(): UseSelection {
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const toggle = useCallback((id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  // togglePage: if every id is already selected → deselect all; otherwise select all.
  const togglePage = useCallback((ids: string[]) => {
    setSelected(prev => {
      const allSelected = ids.length > 0 && ids.every(id => prev.has(id))
      const next = new Set(prev)
      if (allSelected) {
        ids.forEach(id => next.delete(id))
      } else {
        ids.forEach(id => next.add(id))
      }
      return next
    })
  }, [])

  const clear = useCallback(() => setSelected(new Set()), [])

  const isSelected = useCallback((id: string) => selected.has(id), [selected])

  return { selected, toggle, togglePage, clear, isSelected, size: selected.size }
}
