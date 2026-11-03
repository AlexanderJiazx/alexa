import { useEffect, useRef, useState } from 'react'
import { shortcut } from '../../features/palette/shortcuts'
import { fromIme } from '../../lib/ime'
import type { Live as LiveStore } from '../../state/live'
import { shallow, useSelector, type Store } from '../../state/store'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { Moon } from '../../ui/icons'
import type { Action } from './action'
import { clickCount, type Press } from './address'
import type { Live } from './controller'
import {
  buttonBit,
  buttonName,
  chord,
  historyFor,
  holdMs,
  modifiers,
  noZoom,
  pinchZoom,
  toPage,
  touchCancel,
  touchEnd,
  touchHold,
  touchMove,
  touchStart,
  wheelDelta,
  type PagePoint,
  type Point,
  type Touch,
  type TouchOut,
  type Zoom,
} from './input'
import { Pointer } from './Pointer'
import type { HumanInput } from './source'
import { liveStatus } from './status'

// What stays in the keyboard field, so a backspace always has something to
// delete (soft keyboards send nothing for one in an empty field).
const pad = '  '

const viaInput = (e: React.KeyboardEvent) => fromIme(e.nativeEvent) || ['Unidentified', 'Process', 'Dead'].includes(e.key)

// On phones and tablets, text, Backspace and Enter also come through the
// keyboard field: their keyboards don't reliably let a key be held back,
// so taking the key as well would send it twice.
const coarsePointer = matchMedia('(pointer: coarse)')
const editKey = (e: React.KeyboardEvent) => coarsePointer.matches && !e.ctrlKey && !e.metaKey && (e.key.length === 1 || e.key === 'Backspace' || e.key === 'Enter')

const isPaste = (e: React.KeyboardEvent) => (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'v'

// The app's own shortcuts (the palette, Stop, Reflect, the panel) stay the
// app's while you're in control; Escape and every other key go to the page.
const appKey = (e: React.KeyboardEvent) => {
  const id = shortcut({ key: e.key, meta: e.metaKey, ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey, typing: true })
  return !!id && id !== 'escape'
}

// Escape twice in quick succession hands the keyboard back to the app, so
// focus is never trapped in the page (WCAG 2.1.2): the first still reaches
// the page, to close its menus and dialogs.
export const leaveMs = 800
export const leaveHint = 'Press Escape twice to leave the page.'

// capture keeps a pointer's events coming here until it lifts. A pointer
// that's already gone can't be captured; its events simply stop.
const capture = (e: React.PointerEvent<HTMLElement>) => {
  try {
    e.currentTarget.setPointerCapture?.(e.pointerId)
  } catch {
    // The pointer went away before it could be captured.
  }
}

// working: Alexa is at work, so its pointer may act; otherwise it rests.
type Props = { live: Live; control: boolean; cursor: Store<LiveStore>; action: Action | null; working?: boolean }

// LiveScreen is the page itself: the session's shared browser painted onto
// a canvas (the controller paints it, outside React), Alexa's pointer while
// it works, and, while the user is in control, their mouse, touch and
// keyboard forwarded to the page.
export function LiveScreen({ live, control, cursor, action, working = true }: Props) {
  const v = useSelector(live.view, (s) => ({ size: s.size, start: s.start, hasFrame: s.hasFrame, socket: s.socket, status: liveStatus(s) }), shallow)
  const { size } = v
  const boxRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const keysRef = useRef<HTMLTextAreaElement>(null)
  const dotRef = useRef<HTMLSpanElement>(null)
  const [dragging, setDragging] = useState(false)
  const coarse = useRef(coarsePointer.matches)
  // The buttons held as the page knows them, the last point sent, and the
  // last press (for double clicks).
  const held = useRef(0)
  const last = useRef<PagePoint>({ x: 0, y: 0 })
  const press = useRef<Press | null>(null)
  // A move waiting for its turn (moves are paced), and wheel deltas added
  // up until the next animation frame.
  const pending = useRef<HumanInput | null>(null)
  const sentAt = useRef(0)
  const moveTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const wheel = useRef<{ x: number; y: number; dx: number; dy: number } | null>(null)
  const touch = useRef<{ id: number; t: Touch; typing: boolean } | null>(null)
  const holdTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  // The fingers down (in screen pixels), the picture's zoom, and a pinch
  // under way: two fingers zoom and pan the picture (input.ts).
  const fingers = useRef(new Map<number, Point>())
  const zoom = useRef<Zoom>(noZoom)
  const pinch = useRef<{ a0: Point; b0: Point; z0: Zoom } | null>(null)
  // What the keyboard field held after the last change was sent.
  const sentKeys = useRef(pad)
  // When Escape was last pressed, for Escape twice.
  const escAt = useRef(0)
  const composing = useRef(false)

  useEffect(() => {
    const c = canvasRef.current
    return c ? live.attach(c) : undefined
  }, [live])

  // The keyboard field takes the keyboard when control starts (not on
  // phones, where that would throw the keyboard up unasked).
  useEffect(() => {
    const k = keysRef.current
    if (!control || !k) {
      live.setKeys(null)
      live.setTyping(false)
      return
    }
    live.setKeys({ focus: () => k.focus({ preventScroll: true }), blur: () => k.blur() })
    if (!coarse.current) k.focus({ preventScroll: true })
    return () => live.setKeys(null)
  }, [control, live])

  const send = (ev: HumanInput) => {
    if (control) live.send(ev)
  }

  // picture is where the page is drawn on screen: the screen itself, or,
  // zoomed, the canvas's box (its rect includes the zoom).
  const picture = () => (canvasRef.current ?? boxRef.current!).getBoundingClientRect()

  const point = (clientX: number, clientY: number) => {
    const p = toPage(clientX, clientY, picture(), size)
    last.current = p
    return p
  }

  const applyZoom = (z: Zoom) => {
    zoom.current = z
    const c = canvasRef.current
    if (c) c.style.transform = z.k === 1 ? '' : `translate(${z.x}px, ${z.y}px) scale(${z.k})`
  }

  // ---------- the mouse ----------

  const flushMove = () => {
    clearTimeout(moveTimer.current)
    moveTimer.current = undefined
    const m = pending.current
    pending.current = null
    if (m) {
      send(m)
      sentAt.current = performance.now()
    }
  }
  // Moves go at most every 40ms, or every 16ms while a button is held so a
  // drag is smooth, and the last one always goes (FE-04).
  const queueMove = (m: HumanInput) => {
    pending.current = m
    const wait = (m.buttons ? 16 : 40) - (performance.now() - sentAt.current)
    if (wait <= 0) flushMove()
    else if (moveTimer.current === undefined) moveTimer.current = setTimeout(flushMove, wait)
  }

  // release lets go of every button the page thinks is held: the pointer
  // was cancelled or its capture lost (B05).
  const release = () => {
    flushMove()
    for (const b of [0, 2, 1]) {
      const bit = buttonBit(b)
      if (!(held.current & bit)) continue
      held.current &= ~bit
      send({ kind: 'mouseup', ...last.current, button: buttonName(b)!, buttons: held.current, clicks: 1 })
    }
  }

  const onPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!control) return
    // No mouse events after a touch or a click: they'd take the focus from
    // the keyboard field (and, on phones, put the keyboard away).
    e.preventDefault()
    if (e.pointerType === 'touch') return touchDown(e)
    const back = historyFor(e.button)
    if (back) return send({ kind: back })
    const name = buttonName(e.button)
    if (!name) return
    keysRef.current?.focus({ preventScroll: true })
    // Captured, a drag that leaves the screen still ends here (FE-04).
    capture(e)
    flushMove()
    const p = point(e.clientX, e.clientY)
    const now = performance.now()
    const clicks = clickCount(press.current, now, e.clientX, e.clientY, e.button)
    press.current = { at: now, x: e.clientX, y: e.clientY, button: e.button, count: clicks }
    held.current = e.buttons
    send({ kind: 'mousedown', ...p, button: name, buttons: e.buttons, clicks, modifiers: modifiers(e) })
    live.expect()
  }

  const onPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!control) return
    if (e.pointerType === 'touch') return touchMoved(e)
    const p = point(e.clientX, e.clientY)
    // A second button pressed or let go while one is held comes as a move.
    const ch = chord(e.button, e.buttons)
    const name = buttonName(e.button)
    if (ch && name) {
      flushMove()
      held.current = e.buttons
      send({ kind: ch === 'down' ? 'mousedown' : 'mouseup', ...p, button: name, buttons: e.buttons, clicks: 1, modifiers: modifiers(e) })
      return
    }
    queueMove({ kind: 'mousemove', ...p, buttons: e.buttons, modifiers: modifiers(e) })
  }

  const onPointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!control) return
    if (e.pointerType === 'touch') return touchUp(e)
    const name = buttonName(e.button)
    if (!name) return
    flushMove()
    const p = point(e.clientX, e.clientY)
    held.current = e.buttons
    send({ kind: 'mouseup', ...p, button: name, buttons: e.buttons, clicks: press.current?.count ?? 1, modifiers: modifiers(e) })
  }

  const onPointerCancel = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!control) return
    if (e.pointerType === 'touch') return touchCancelled(e)
    release()
  }

  // ---------- the wheel and touch scrolling ----------

  // Wheel deltas add up until the next frame, so a fast trackpad doesn't
  // queue a hundred scrolls behind each other (B27).
  const queueWheel = (p: PagePoint, dx: number, dy: number) => {
    const w = wheel.current
    if (w) {
      wheel.current = { ...p, dx: w.dx + dx, dy: w.dy + dy }
      return
    }
    wheel.current = { ...p, dx, dy }
    requestAnimationFrame(() => {
      const q = wheel.current
      wheel.current = null
      if (q) send({ kind: 'wheel', x: q.x, y: q.y, dx: q.dx, dy: q.dy })
    })
  }

  // The wheel listener isn't passive, so scrolling the page doesn't scroll
  // the app around it as well.
  useEffect(() => {
    const box = boxRef.current
    if (!box || !control) return
    const on = (e: WheelEvent) => {
      e.preventDefault()
      const r = picture()
      const d = wheelDelta(e.deltaX, e.deltaY, e.deltaMode, size, size.w / r.width)
      queueWheel(toPage(e.clientX, e.clientY, r, size), d.dx, d.dy)
    }
    box.addEventListener('wheel', on, { passive: false })
    return () => box.removeEventListener('wheel', on)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [control, size])

  // ---------- touch: tap, swipe, or hold then drag ----------

  const placeDot = (x: number, y: number) => {
    const d = dotRef.current
    const r = boxRef.current?.getBoundingClientRect()
    if (!d || !r) return
    d.style.left = `${x - r.left}px`
    d.style.top = `${y - r.top}px`
  }

  const runTouch = (outs: TouchOut[]) => {
    for (const o of outs) {
      const p = point(o.x, o.y)
      if (o.kind === 'tap') {
        send({ kind: 'mousedown', ...p, button: 'left', buttons: 1, clicks: 1 })
        send({ kind: 'mouseup', ...p, button: 'left', buttons: 0, clicks: 1 })
        live.expect()
      } else if (o.kind === 'scroll') {
        const r = picture()
        queueWheel(p, o.dx * (size.w / r.width), o.dy * (size.w / r.width))
      } else if (o.kind === 'down') {
        held.current = 1
        send({ kind: 'mousedown', ...p, button: 'left', buttons: 1, clicks: 1 })
        navigator.vibrate?.(8)
        placeDot(o.x, o.y)
        setDragging(true)
        live.expect()
      } else if (o.kind === 'move') {
        placeDot(o.x, o.y)
        queueMove({ kind: 'mousemove', ...p, buttons: 1 })
      } else {
        flushMove()
        held.current = 0
        send({ kind: 'mouseup', ...p, button: 'left', buttons: 0, clicks: 1 })
        setDragging(false)
      }
    }
  }

  const local = (e: React.PointerEvent<HTMLDivElement>): Point => {
    const r = boxRef.current!.getBoundingClientRect()
    return { x: e.clientX - r.left, y: e.clientY - r.top }
  }

  const touchDown = (e: React.PointerEvent<HTMLDivElement>) => {
    fingers.current.set(e.pointerId, local(e))
    // A second finger, unless the first is dragging: the two pinch, and
    // the first one's tap or swipe is off.
    if (fingers.current.size === 2 && touch.current?.t.phase !== 'drag') {
      capture(e)
      clearTimeout(holdTimer.current)
      if (touch.current) runTouch(touchCancel(touch.current.t)) // nothing for a tap or a swipe
      touch.current = null
      const [a0, b0] = [...fingers.current.values()]
      pinch.current = { a0, b0, z0: zoom.current }
      return
    }
    if (touch.current || pinch.current) return // more fingers: the first ones lead
    capture(e)
    touch.current = { id: e.pointerId, t: touchStart(e.clientX, e.clientY), typing: document.activeElement === keysRef.current }
    clearTimeout(holdTimer.current)
    holdTimer.current = setTimeout(() => {
      const tc = touch.current
      if (!tc) return
      const [t, out] = touchHold(tc.t)
      tc.t = t
      runTouch(out)
    }, holdMs)
  }
  const touchMoved = (e: React.PointerEvent<HTMLDivElement>) => {
    if (fingers.current.has(e.pointerId)) fingers.current.set(e.pointerId, local(e))
    const pz = pinch.current
    if (pz) {
      const [a, b] = [...fingers.current.values()]
      const r = boxRef.current!.getBoundingClientRect()
      if (a && b) applyZoom(pinchZoom(pz.z0, pz.a0, pz.b0, a, b, r.width, r.height))
      return
    }
    const tc = touch.current
    // Only the finger that leads moves the page.
    if (!tc || tc.id !== e.pointerId) return
    const [t, out] = touchMove(tc.t, e.clientX, e.clientY)
    if (t.phase !== 'pending') clearTimeout(holdTimer.current)
    tc.t = t
    runTouch(out)
  }
  // A finger lifting ends a pinch; the one still down does nothing until
  // it lifts too.
  const fingerGone = (id: number) => {
    fingers.current.delete(id)
    if (pinch.current && fingers.current.size < 2) pinch.current = null
  }
  const touchUp = (e: React.PointerEvent<HTMLDivElement>) => {
    fingerGone(e.pointerId)
    const tc = touch.current
    if (!tc || tc.id !== e.pointerId) return
    clearTimeout(holdTimer.current)
    touch.current = null
    runTouch(touchEnd(tc.t, e.clientX, e.clientY))
    // Tapping the page keeps the on-screen keyboard up if it was.
    if (tc.typing) keysRef.current?.focus({ preventScroll: true })
  }
  const touchCancelled = (e: React.PointerEvent<HTMLDivElement>) => {
    fingerGone(e.pointerId)
    const tc = touch.current
    if (!tc || tc.id !== e.pointerId) return
    clearTimeout(holdTimer.current)
    touch.current = null
    runTouch(touchCancel(tc.t))
  }

  // Nothing stays held, or zoomed, when control ends or the view closes.
  useEffect(() => {
    if (control) return
    clearTimeout(holdTimer.current)
    touch.current = null
    fingers.current.clear()
    pinch.current = null
    applyZoom(noZoom)
    held.current = 0
    setDragging(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [control])
  useEffect(() => () => clearTimeout(moveTimer.current), [])

  // ---------- the keyboard ----------

  const pressKey = (key: string, code: string, keyCode: number, text = '') => {
    send({ kind: 'keydown', key, code, keyCode, text })
    send({ kind: 'keyup', key, code, keyCode })
  }

  // type sends text, turning line breaks into Enter.
  const type = (text: string) => {
    text.split(/\r?\n/).forEach((line, i) => {
      if (i > 0) pressKey('Enter', 'Enter', 13, '\r')
      if (line) send({ kind: 'text', text: line })
    })
    live.expect()
  }

  const resetKeys = () => {
    const k = keysRef.current
    if (!k) return
    k.value = pad
    k.setSelectionRange(pad.length, pad.length)
    sentKeys.current = pad
  }

  // What soft keyboards and input methods type arrives as changes to the
  // keyboard field; each is sent on as backspaces and text.
  const onInput = () => {
    const k = keysRef.current
    if (!k) return
    const now = k.value
    const before = sentKeys.current
    let same = 0
    while (same < now.length && same < before.length && now[same] === before[same]) same++
    for (let i = same; i < before.length; i++) pressKey('Backspace', 'Backspace', 8)
    if (now.length > same) type(now.slice(same).replace(/ /g, ' '))
    sentKeys.current = now
    if (!composing.current) resetKeys()
  }

  // leave puts the keyboard back in the app, on the button that hands the
  // browser back.
  const leave = () => {
    keysRef.current?.blur()
    const box = boxRef.current?.closest('.bp')
    box?.querySelector<HTMLElement>('.bp-handback button')?.focus()
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (!control) return
    // Input methods, accent keys and soft keyboards that don't say which key
    // it was type through onInput; the browser handles paste so the text
    // can be sent.
    if (viaInput(e) || editKey(e) || isPaste(e) || appKey(e)) return
    e.preventDefault()
    if (e.key === 'Escape' && !e.repeat && !modifiers(e)) {
      const again = escAt.current > 0 && e.timeStamp - escAt.current < leaveMs
      escAt.current = again ? 0 : e.timeStamp
      if (again) {
        // The page already had the first Escape; this one is the app's.
        leave()
        return
      }
    } else escAt.current = 0
    const printable = e.key.length === 1 && !e.ctrlKey && !e.metaKey
    send({ kind: 'keydown', key: e.key, code: e.code, keyCode: e.keyCode, text: printable ? e.key : e.key === 'Enter' ? '\r' : '', modifiers: modifiers(e) })
    live.expect()
  }
  const onKeyUp = (e: React.KeyboardEvent) => {
    if (!control || viaInput(e) || editKey(e) || isPaste(e) || appKey(e)) return
    e.preventDefault()
    send({ kind: 'keyup', key: e.key, code: e.code, keyCode: e.keyCode, modifiers: modifiers(e) })
  }
  const onPaste = (e: React.ClipboardEvent) => {
    if (!control) return
    e.preventDefault()
    const t = e.clipboardData.getData('text')
    if (t) type(t)
  }

  const { status } = v
  return (
    <div
      ref={boxRef}
      className={cx('screen', control && 'control', `is-${status}`, !v.hasFrame && 'empty')}
      role={control ? 'application' : 'img'}
      aria-label={control ? `Shared browser. You’re in control: what you click and type goes to the page. ${leaveHint}` : 'Shared browser'}
      aria-roledescription={control ? 'remote page' : undefined}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
      onLostPointerCapture={(e) => e.pointerType !== 'touch' && control && held.current && release()}
      onContextMenu={(e) => control && e.preventDefault()}
    >
      <canvas ref={canvasRef} className="bp-canvas" aria-hidden="true" />
      {!v.hasFrame && <Placeholder status={status} open={v.socket === 'open'} />}
      {v.hasFrame && status === 'reconnecting' && (
        <div className="bp-veil" role="status">
          <span className="bp-chip">
            <span className="spinner" aria-hidden="true" />
            Reconnecting…
          </span>
        </div>
      )}
      {status === 'asleep' && <Asleep onWake={() => live.wake()} />}
      {!control && status !== 'asleep' && status !== 'reconnecting' && <Pointer cursor={cursor} size={size} start={v.start} action={action} resting={!working} />}
      {control && (
        <textarea
          ref={keysRef}
          className="keys"
          aria-label={`Type into the page. ${leaveHint}`}
          defaultValue={pad}
          autoCapitalize="none"
          autoCorrect="off"
          autoComplete="off"
          spellCheck={false}
          onFocus={() => {
            resetKeys()
            live.setTyping(true)
          }}
          onBlur={() => live.setTyping(false)}
          onKeyDown={onKeyDown}
          onKeyUp={onKeyUp}
          onInput={onInput}
          onCompositionStart={() => (composing.current = true)}
          onCompositionEnd={() => {
            composing.current = false
            onInput()
          }}
          onPaste={onPaste}
        />
      )}
      {control && <span ref={dotRef} className={cx('bp-touch', dragging && 'on')} aria-hidden="true" />}
    </div>
  )
}

// Placeholder holds the page's place until the first frame: the shape of a
// page, shimmering, and what's happening.
function Placeholder({ status, open }: { status: string; open: boolean }) {
  const text = status === 'reconnecting' ? 'Reconnecting…' : open ? 'Opening the browser…' : 'Connecting to the browser…'
  return (
    <div className="bp-empty" role="status">
      <span className="bp-empty-page" aria-hidden="true">
        <span className="skel bp-empty-bar" />
        <span className="bp-empty-cols">
          <span className="skel block" />
          <span className="bp-empty-lines">
            <span className="skel line" />
            <span className="skel line" />
            <span className="skel line short" />
          </span>
        </span>
      </span>
      <span className="bp-empty-text">{text}</span>
    </div>
  )
}

// Asleep is the browser put away after a while unused (B10). It wakes when
// Alexa next needs it, or now.
function Asleep({ onWake }: { onWake: () => void }) {
  return (
    <div className="bp-veil asleep" role="status">
      <div className="bp-card">
        <span className="bp-card-ic" aria-hidden="true">
          <Moon size={20} />
        </span>
        <p className="bp-card-title">The browser is asleep</p>
        <p className="bp-card-line">It wakes up when Alexa needs it.</p>
        <Button size="sm" onClick={onWake}>
          Wake it now
        </Button>
      </div>
    </div>
  )
}
