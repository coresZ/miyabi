import { useState } from 'react'

export function useHistorySelection<T extends { id: number }>(items: T[]) {
  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(() => new Set())

  const selectedIDs = items.filter(item => selected.has(item.id)).map(item => item.id)
  const allSelected = items.length > 0 && selectedIDs.length === items.length

  function leaveSelection() {
    setSelecting(false)
    setSelected(new Set())
  }

  function toggleSelect(id: number) {
    setSelected(current => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function toggleSelectAll() {
    setSelected(allSelected ? new Set() : new Set(items.map(item => item.id)))
  }

  return {
    selecting,
    setSelecting,
    selected,
    selectedIDs,
    allSelected,
    leaveSelection,
    toggleSelect,
    toggleSelectAll
  }
}
