import { useEffect, useState } from 'react'

const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)')

// useExit keeps showing a value for ms after it goes away, so what shows it
// can animate out. It returns the value to show and whether it's leaving.
export function useExit<T>(value: T | null, ms: number): { shown: T | null; leaving: boolean } {
  const [last, setLast] = useState(value)
  if (value !== null && value !== last) setLast(value)
  useEffect(() => {
    if (value !== null || last === null) return
    const t = setTimeout(() => setLast(null), reduceMotion.matches ? 0 : ms)
    return () => clearTimeout(t)
  }, [value, last, ms])
  return { shown: value ?? last, leaving: value === null && last !== null }
}
