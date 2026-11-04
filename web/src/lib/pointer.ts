import { useEffect, useState } from 'react'

const QUERY = '(pointer: coarse)'

/**
 * Layout choice follows the input device, not the window width: a resized
 * desktop window must not turn the card into its touch behaviour.
 */
export function useCoarsePointer(): boolean {
  const [coarse, setCoarse] = useState(() => window.matchMedia(QUERY).matches)
  useEffect(() => {
    const query = window.matchMedia(QUERY)
    const update = () => setCoarse(query.matches)
    update()
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])
  return coarse
}
