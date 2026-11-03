import { useLayoutEffect, useRef, useState } from 'react'

// useWidth is an element's width in CSS pixels, kept up to date as its
// column resizes. first is the width to assume before it's measured (and
// when rendering without a DOM).
export function useWidth<T extends HTMLElement>(first: number) {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(first)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const set = () => {
      const w = Math.round(el.getBoundingClientRect().width)
      if (w > 0) setWidth(w)
    }
    set()
    const ro = new ResizeObserver(set)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, width] as const
}
