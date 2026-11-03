import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from 'react'
import { useMedia } from '../lib/useMedia'
import { closePanel } from '../state/actions'
import { useLive } from '../state/live'
import { useApp } from '../state/store'
import { cx } from '../ui/cx'
import { detentsFor, rank, shortScreen, stepDetent, type Detent } from './detents'
import { PanelHost } from './PanelHost'

// On screens under 900px the panel is a bottom sheet over the
// conversation (IX-F4), with three heights (detents.ts): peek (its head and
// a sliver), half (the conversation above it, the composer below), and
// full. A short screen has only the peek and full.
export { detents, type Detent } from './detents'

// The peek is the grabber, the panel's head and a sliver of what's in it,
// above the composer.
export const peekPx = 96

type Props = {
  panel: string
  leaving: boolean
  detent: Detent
  onDetent: (d: Detent) => void
  // foot sits between the panel and the composer: the caption, at full
  // height, when the conversation is out of sight.
  foot?: ReactNode
}

type Drag = { id: number; y0: number; from: number; y: number; t: number; v: number; moved: boolean; H: number; stops: number[] }

// What a drag can start on: the grabber or the panel's head, not the
// buttons in it.
const handle = (t: EventTarget) => t instanceof Element && !!t.closest('.psheet-grab, .panel-head, .psheet-peek') && !t.closest('a, input, select, textarea, [role=switch], button:not(.psheet-grab)')

// tapDetent is where a tap on the grabber or the head takes the sheet:
// between half and full (up to half from the peek), or between the peek
// and full on a short screen.
export function tapDetent(d: Detent, short: boolean): Detent {
  if (short) return d === 'full' ? 'peek' : 'full'
  return d === 'half' ? 'full' : 'half'
}

// PanelSheet holds the one panel on a small screen. Drag its grabber or head
// between the heights (a fling carries it on; down past the peek closes it),
// tap the grabber or the head to switch between half and full (the peek and
// full on a short screen), or use the arrow keys on the grabber. The
// content is resized to what shows, growing before the sheet rises and
// shrinking after it settles, so nothing reflows in view.
export function PanelSheet({ panel, leaving, detent, onDetent, foot }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const dock = useRef<HTMLDivElement>(null)
  const drag = useRef<Drag | null>(null)
  const dragged = useRef(false)
  const [y, setY] = useState<number | null>(null)
  const [content, setContent] = useState<Detent>(detent)
  const dragging = y !== null
  const short = useMedia(shortScreen)
  // What half became on a short screen, to give back when there's room.
  const lowered = useRef(false)

  useEffect(() => {
    if (short && detent === 'half') {
      lowered.current = true
      onDetent('peek')
    } else if (!short && lowered.current) {
      lowered.current = false
      if (detent === 'peek') onDetent('half')
    } else if (detent !== 'peek') lowered.current = false
  }, [short, detent, onDetent])

  useEffect(() => {
    if (dragging || detent === content) return
    if (rank(detent) > rank(content)) {
      setContent(detent)
      return
    }
    const t = setTimeout(() => setContent(detent), 360)
    return () => clearTimeout(t)
  }, [detent, content, dragging])

  const onDown = (e: PointerEvent<HTMLDivElement>) => {
    const el = ref.current
    if (!el || e.button !== 0 || !handle(e.target)) return
    const H = el.offsetHeight
    const peek = peekPx + (dock.current?.offsetHeight ?? 0)
    const stops = short ? [peek, H] : [peek, Math.round(H / 2), H]
    const from = H - stops[Math.max(0, detentsFor(short).indexOf(detent))]
    drag.current = { id: e.pointerId, y0: e.clientY, from, y: from, t: e.timeStamp, v: 0, moved: false, H, stops }
    dragged.current = false
  }

  const onMove = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current
    if (!d || d.id !== e.pointerId) return
    const dy = e.clientY - d.y0
    if (!d.moved) {
      if (Math.abs(dy) < 6) return
      d.moved = true
      e.currentTarget.setPointerCapture(e.pointerId)
      setContent('full')
    }
    // Past the top it gives, like a rubber band.
    const next = d.from + dy < 0 ? (d.from + dy) / 3 : d.from + dy
    const dt = Math.max(1, e.timeStamp - d.t)
    d.v = 0.8 * ((next - d.y) / dt) + 0.2 * d.v
    d.y = next
    d.t = e.timeStamp
    setY(next)
  }

  const onUp = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current
    if (!d || d.id !== e.pointerId) return
    drag.current = null
    if (!d.moved) {
      // A tap on the head does what one on the grabber does, at every
      // height: the 24px grabber alone is too thin a target for a finger,
      // and growing it would make the peek taller. A touch the browser took
      // back (a long press, a system gesture) wasn't a tap.
      if (e.type === 'pointerup' && !(e.target instanceof Element && e.target.closest('.psheet-grab'))) onDetent(tapDetent(detent, short))
      return
    }
    dragged.current = true
    setY(null)
    // Where the fling would carry it, as a visible height.
    const end = d.H - (d.y + d.v * 180)
    if (end < d.stops[0] * 0.55) return closePanel()
    let best = 0
    d.stops.forEach((s, i) => {
      if (Math.abs(s - end) < Math.abs(d.stops[best] - end)) best = i
    })
    onDetent(detentsFor(d.stops.length === 2)[best])
  }

  const toggle = () => {
    if (dragged.current) {
      dragged.current = false
      return
    }
    onDetent(tapDetent(detent, short))
  }

  const onKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    const by = e.key === 'ArrowUp' ? 1 : e.key === 'ArrowDown' ? -1 : 0
    const next = by ? stepDetent(detent, by, short) : detent
    if (next === detent) return
    onDetent(next)
    e.preventDefault()
  }

  return (
    <div ref={ref} className={cx('psheet', `is-${panel.split(':')[0]}`, detent, `c-${content}`, dragging && 'dragging', leaving && 'closing')} style={dragging ? { transform: `translateY(${y}px)` } : undefined}>
      <div className="psheet-in" onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp}>
        <button type="button" className="psheet-grab" aria-label={detent === 'full' ? 'Make the panel smaller' : 'Make the panel bigger'} aria-keyshortcuts="ArrowUp ArrowDown" onClick={toggle} onKeyDown={onKey}>
          <span />
        </button>
        <PanelHost panel={panel} />
        {content === 'peek' && <PeekTitle panel={panel} />}
        {foot && <div className="psheet-foot">{foot}</div>}
        <div ref={dock} className="psheet-dock" aria-hidden="true" />
      </div>
    </div>
  )
}

// PeekTitle names what's in the sheet while only its head shows: the
// report's title, or the page's in the browser. An expanded entry's head
// names it already.
function PeekTitle({ panel }: { panel: string }) {
  const report = panel.startsWith('report:') ? panel.slice(7) : ''
  const reportTitle = useApp((s) => (report ? s.data.reports[report]?.title : undefined))
  const pageTitle = useLive((l) => (panel === 'browser' ? l.title : ''))
  const title = reportTitle || pageTitle
  if (!title) return null
  return (
    <p className="psheet-peek" aria-hidden="true">
      {title}
    </p>
  )
}
