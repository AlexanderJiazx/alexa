import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { cx } from '../../../ui/cx'
import { Back, Fwd } from '../../../ui/icons'

// Carousel is a row of cards that scrolls sideways and snaps to each card.
// The row itself takes focus, so the arrow keys move it; Tab still walks
// every card's links and buttons, scrolling them into view. On a desktop
// pointer, round arrows sit at the edges; the edges fade where there's
// more. It never scrolls vertically.
export function Carousel({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  const ref = useRef<HTMLUListElement>(null)
  const [edge, setEdge] = useState({ start: true, end: true })

  const measure = useCallback(() => {
    const el = ref.current
    if (!el) return
    const start = el.scrollLeft <= 2
    const end = el.scrollLeft + el.clientWidth >= el.scrollWidth - 2
    setEdge((e) => (e.start === start && e.end === end ? e : { start, end }))
  }, [])

  useEffect(() => {
    const el = ref.current
    if (!el) return
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [measure])

  // step moves by whole cards: as many as fit.
  const step = (dir: 1 | -1) => {
    const el = ref.current
    const card = el?.firstElementChild as HTMLElement | null
    if (!el || !card) return
    const gap = parseFloat(getComputedStyle(el).columnGap) || 0
    const w = card.getBoundingClientRect().width + gap
    const n = Math.max(1, Math.floor((el.clientWidth + gap) / w))
    el.scrollBy({ left: dir * n * w, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
  }

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
    const t = e.target as HTMLElement
    if (t.matches('input, textarea, select')) return
    e.preventDefault()
    step(e.key === 'ArrowRight' ? 1 : -1)
  }

  return (
    <div className={cx('b-carousel', edge.start && 'at-start', edge.end && 'at-end', className)}>
      <ul ref={ref} className="b-track" role="list" tabIndex={0} aria-label={label} onScroll={measure} onKeyDown={onKeyDown}>
        {children}
      </ul>
      <button type="button" className="b-arrow prev" tabIndex={-1} aria-label="Previous" onClick={() => step(-1)} hidden={edge.start}>
        <Back size={18} />
      </button>
      <button type="button" className="b-arrow next" tabIndex={-1} aria-label="Next" onClick={() => step(1)} hidden={edge.end}>
        <Fwd size={18} />
      </button>
    </div>
  )
}
