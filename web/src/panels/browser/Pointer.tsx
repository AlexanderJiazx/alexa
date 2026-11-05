import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Cursor, Live as LiveStore } from '../../state/live'
import { useSelector, type Store } from '../../state/store'
import { CursorArrow } from '../../ui/icons'
import type { Action } from './action'
import { ActionBubble, TargetRing, type Box } from './ActionOverlay'
import { glide, pathFor, pointAt, type Point } from './glide'
import { cx } from '../../ui/cx'
import type { Size } from './input'

const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)')

// A pointer move may carry the box of the element it's heading for
// (proposed, WP-11): the server doesn't send it yet, so no ring is drawn.
type BoxedCursor = Cursor & { box?: Box }

// How long the pointer stays once Alexa has stopped working, so its last
// click lands, before it fades away.
export const restMs = 2000

// resting: Alexa isn't working, so the pointer isn't about to do anything.
type Props = { cursor: Store<LiveStore>; size: Size; start: Point | null; action: Action | null; resting?: boolean }

// Pointer is Alexa's pointer on the page: the accent arrow with its name,
// gliding to where it acts and rippling when it clicks, with what it's
// doing riding along beside it. Each step of a glide is drawn straight onto
// the element; only a new move goes through React. While Alexa isn't
// working it isn't left lying on the page as if about to click: it fades a
// moment after the run ends, and a view opened then doesn't show it until
// it next moves.
export function Pointer({ cursor, size, start, action, resting = false }: Props) {
  const c = useSelector(cursor, (l) => l.cursor) as BoxedCursor | null
  const el = useRef<HTMLSpanElement>(null)
  // Where the pointer is drawn, in page pixels, and the glide under way.
  const at = useRef<Point | null>(null)
  const raf = useRef(0)
  // The moves already shown: one from before this view opened isn't
  // played again.
  const seen = useRef(c?.seq ?? 0)
  const sizeRef = useRef(size)
  sizeRef.current = size
  const side = useRef<HTMLSpanElement>(null)
  // How wide the screen and the name-and-action are, in CSS pixels:
  // measured when either changes, never during a glide.
  const room = useRef({ screen: 0, side: 0 })
  const [away, setAway] = useState(resting)
  const restSeq = useRef(c?.seq ?? 0)
  useEffect(() => {
    const moved = (c?.seq ?? 0) !== restSeq.current
    restSeq.current = c?.seq ?? 0
    if (!resting) {
      setAway(false)
      return
    }
    // A move while resting shows, then goes again.
    if (moved) setAway(false)
    const t = setTimeout(() => setAway(true), restMs)
    return () => clearTimeout(t)
  }, [resting, c?.seq])

  const place = (p: Point) => {
    at.current = p
    const e = el.current
    if (!e) return
    const { w, h } = sizeRef.current
    e.style.left = `${(p.x / w) * 100}%`
    e.style.top = `${(p.y / h) * 100}%`
    // Near the right or bottom edge, the name and the action sit on the
    // other side of the arrow so they stay on the page: on the right when
    // they fit there, else on whichever side has more room.
    const { screen, side: wide } = room.current
    const px = (p.x / w) * screen
    const flipX = screen ? px + 12 + wide > screen - 4 && px > screen - px : p.x > w * 0.66
    e.classList.toggle('flip-x', flipX)
    e.classList.toggle('flip-y', p.y > h * 0.78)
  }

  const measure = () => {
    const e = el.current
    room.current = { screen: e?.parentElement?.clientWidth ?? 0, side: side.current?.offsetWidth ?? 0 }
    if (at.current) place(at.current)
  }

  // The action changes what rides beside the pointer, and the panel can
  // change size: measure again for either.
  useLayoutEffect(measure)
  useEffect(() => {
    const screen = el.current?.parentElement
    if (!screen || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => measure())
    ro.observe(screen)
    return () => ro.disconnect()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [!!(c || start)])

  // Opening the view, or back from a take-over, the pointer is just where
  // it was: where it was drawn, else where the server says it is, else
  // where it last went.
  useLayoutEffect(() => {
    const p = at.current ?? start ?? (c && { x: c.x, y: c.y })
    if (p) place(p)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [size.w, size.h, !!c, !!start])

  // Each move glides along a slight curve and clicks when it gets there.
  useEffect(() => {
    const e = el.current
    if (!c || !e || c.seq === seen.current) return
    seen.current = c.seq
    const to = { x: c.x, y: c.y }
    const from = at.current ?? start ?? { x: sizeRef.current.w / 2, y: sizeRef.current.h / 2 }
    const ms = reduceMotion.matches ? 0 : glide(Math.hypot(to.x - from.x, to.y - from.y))
    const path = pathFor(from, to, c.seq % 2 ? 1 : -1)
    const click = () => {
      if (!c.click) return
      e.classList.remove('click')
      void e.offsetWidth // restart the animation
      e.classList.add('click')
    }
    cancelAnimationFrame(raf.current)
    if (!ms) {
      place(to)
      click()
      return
    }
    const t0 = performance.now()
    const step = (now: number) => {
      const t = Math.min(1, (now - t0) / ms)
      place(pointAt(path, t))
      if (t < 1) raf.current = requestAnimationFrame(step)
      else click()
    }
    raf.current = requestAnimationFrame(step)
    return () => cancelAnimationFrame(raf.current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [c?.seq])

  // What Alexa does to the page as a whole, or with no pointer to ride
  // beside, sits at the foot of the page.
  const atPointer = !!action && !action.page && !!(c || start)
  return (
    <>
      {action && !atPointer && <ActionBubble action={action} floating />}
      {atPointer && c?.box && <TargetRing box={c.box} size={size} />}
      {(c || start) && (
        <span ref={el} className={cx('bp-pointer', away && 'away')} aria-hidden="true">
          <span className="rip" />
          <CursorArrow />
          <span ref={side} className="bp-side">
            <span className="bp-tag">Alexa</span>
            {atPointer && <ActionBubble action={action} />}
          </span>
        </span>
      )}
    </>
  )
}
