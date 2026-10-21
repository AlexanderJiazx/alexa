import { useEffect, useRef, useState } from 'react'
import { wsURL } from '../lib/api'
import { Back, CursorArrow, Fwd, Keyboard, Lock, Reload } from './Icons'

export type Cursor = { x: number; y: number; click: boolean; seq: number }

type Props = {
  control: boolean
  cursor: Cursor | null
  url: string
  onUrl: (url: string, title?: string) => void
}

type Point = { x: number; y: number }

// glide is how long the pointer takes to travel dist pixels of the page; the
// server waits as long before acting, so the page reacts as it arrives.
export function glide(dist: number) {
  return dist < 1 ? 0 : Math.min(220 + 0.3 * dist, 700)
}

// A reach: slow start, quick middle, gentle landing (minimum jerk).
const reach = (t: number) => t * t * t * (10 - 15 * t + 6 * t * t)

const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)')

// What stays in the keyboard field, so a backspace always has something to
// delete (soft keyboards send nothing for one in an empty field).
const pad = '  '

const viaInput = (e: React.KeyboardEvent) => e.nativeEvent.isComposing || e.keyCode === 229 || ['Unidentified', 'Process', 'Dead'].includes(e.key)

// On phones and tablets, text, Backspace and Enter also come through the
// keyboard field: their keyboards don't reliably let a key be held back,
// so taking the key as well would send it twice.
const coarsePointer = matchMedia('(pointer: coarse)')
const editKey = (e: React.KeyboardEvent) => coarsePointer.matches && !e.ctrlKey && !e.metaKey && (e.key.length === 1 || e.key === 'Backspace' || e.key === 'Enter')

// LiveScreen streams the session's shared browser as JPEG frames and, when
// the user is in control, forwards their mouse and keyboard to it.
export function LiveScreen({ control, cursor, url, onUrl }: Props) {
  const [src, setSrc] = useState<string | null>(null)
  const [size, setSize] = useState({ w: 1280, h: 800 })
  const [connected, setConnected] = useState(false)
  const [editUrl, setEditUrl] = useState<string | null>(null)
  const [typing, setTyping] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const keysRef = useRef<HTMLTextAreaElement>(null)
  const pointerRef = useRef<HTMLSpanElement>(null)
  const lastMove = useRef(0)
  const touch = useRef<{ x: number; y: number; moved: boolean; typing: boolean } | null>(null)
  const onUrlRef = useRef(onUrl)
  onUrlRef.current = onUrl
  // Where Alexa's pointer is drawn, in page pixels, and the glide under way.
  const at = useRef<Point | null>(null)
  const start = useRef<Point | null>(null)
  const frame = useRef(0)
  // The pointer moves that have been shown; one from before this view
  // opened isn't played again.
  const seen = useRef(cursor?.seq ?? 0)
  const sizeRef = useRef(size)
  sizeRef.current = size
  // What the keyboard field held after the last change was sent.
  const sent = useRef(pad)
  const composing = useRef(false)
  const coarse = useRef(coarsePointer.matches)

  useEffect(() => {
    let stopped = false
    let current: string | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    const connect = () => {
      const ws = new WebSocket(wsURL('/api/live'))
      ws.binaryType = 'blob'
      wsRef.current = ws
      ws.onopen = () => {
        attempt = 0
        setConnected(true)
      }
      ws.onmessage = (ev) => {
        if (typeof ev.data === 'string') {
          try {
            const m = JSON.parse(ev.data)
            if (m.type === 'hello') {
              setSize({ w: m.width, h: m.height })
              if (m.url) onUrlRef.current(m.url, m.title)
              if (m.cursor) start.current = { x: m.cursor.x, y: m.cursor.y }
            }
          } catch {
            /* ignore */
          }
          return
        }
        const next = URL.createObjectURL(ev.data as Blob)
        setSrc(next)
        if (current) URL.revokeObjectURL(current)
        current = next
      }
      ws.onclose = () => {
        setConnected(false)
        if (stopped) return
        attempt++
        timer = setTimeout(connect, Math.min(10000, 600 * 2 ** attempt))
      }
    }
    connect()
    return () => {
      stopped = true
      clearTimeout(timer)
      wsRef.current?.close()
      if (current) URL.revokeObjectURL(current)
    }
  }, [])

  // Alexa's pointer glides to where it acts, along a slight curve, and
  // clicks when it gets there.
  const place = (p: Point) => {
    at.current = p
    const el = pointerRef.current
    if (!el) return
    el.style.left = `${(p.x / sizeRef.current.w) * 100}%`
    el.style.top = `${(p.y / sizeRef.current.h) * 100}%`
  }
  useEffect(() => {
    const el = pointerRef.current
    if (!cursor || control || !el) return
    const to = { x: cursor.x, y: cursor.y }
    // Opening the view, or back from a take-over, the pointer is just where
    // it was.
    if (cursor.seq === seen.current) {
      place(to)
      return
    }
    seen.current = cursor.seq
    const from = at.current ?? start.current ?? { x: sizeRef.current.w / 2, y: sizeRef.current.h / 2 }
    const dx = to.x - from.x
    const dy = to.y - from.y
    const dist = Math.hypot(dx, dy)
    const ms = reduceMotion.matches ? 0 : glide(dist)
    // Bend the path a little to one side, as a hand would.
    const bend = Math.min(dist * 0.16, 56) * (cursor.seq % 2 ? 1 : -1)
    const c = { x: from.x + dx / 2 - (dy / (dist || 1)) * bend, y: from.y + dy / 2 + (dx / (dist || 1)) * bend }
    const click = () => {
      if (!cursor.click) return
      el.classList.remove('click')
      void el.offsetWidth // restart the animation
      el.classList.add('click')
    }
    cancelAnimationFrame(frame.current)
    if (!ms) {
      place(to)
      click()
      return
    }
    const t0 = performance.now()
    const step = (now: number) => {
      const t = Math.min(1, (now - t0) / ms)
      const e = reach(t)
      const u = 1 - e
      place({ x: u * u * from.x + 2 * u * e * c.x + e * e * to.x, y: u * u * from.y + 2 * u * e * c.y + e * e * to.y })
      if (t < 1) frame.current = requestAnimationFrame(step)
      else click()
    }
    frame.current = requestAnimationFrame(step)
    return () => cancelAnimationFrame(frame.current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cursor?.seq, control])

  // The pointer keeps its place when the frame size arrives or changes.
  useEffect(() => {
    if (at.current) place(at.current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [size.w, size.h])

  useEffect(() => {
    const k = keysRef.current
    if (control && k && !coarse.current) k.focus({ preventScroll: true })
    if (!control) {
      k?.blur()
      setTyping(false)
    }
  }, [control])

  const send = (ev: Record<string, unknown>) => {
    const ws = wsRef.current
    if (control && ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(ev))
  }

  const toPage = (clientX: number, clientY: number) => {
    const r = boxRef.current!.getBoundingClientRect()
    const scale = Math.min(r.width / size.w, r.height / size.h)
    const offX = (r.width - size.w * scale) / 2
    return { x: Math.round((clientX - r.left - offX) / scale), y: Math.round((clientY - r.top) / scale) }
  }

  const mods = (e: { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }) =>
    (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0)

  const btn = (b: number) => (b === 2 ? 'right' : b === 1 ? 'middle' : 'left')

  const press = (key: string, code: string, keyCode: number, text = '') => {
    send({ kind: 'keydown', key, code, keyCode, text })
    send({ kind: 'keyup', key, code, keyCode })
  }

  // type sends text, turning line breaks into Enter.
  const type = (text: string) => {
    text.split(/\r?\n/).forEach((line, i) => {
      if (i > 0) press('Enter', 'Enter', 13, '\r')
      if (line) send({ kind: 'text', text: line })
    })
  }

  const resetKeys = () => {
    const k = keysRef.current
    if (!k) return
    k.value = pad
    k.setSelectionRange(pad.length, pad.length)
    sent.current = pad
  }

  // What soft keyboards and input methods type arrives as changes to the
  // keyboard field; each is sent on as backspaces and text.
  const onInput = () => {
    const k = keysRef.current
    if (!k) return
    const now = k.value
    const before = sent.current
    let same = 0
    while (same < now.length && same < before.length && now[same] === before[same]) same++
    for (let i = same; i < before.length; i++) press('Backspace', 'Backspace', 8)
    if (now.length > same) type(now.slice(same).replace(/ /g, ' '))
    sent.current = now
    if (!composing.current) resetKeys()
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (!control) return
    // Input methods, accent keys and soft keyboards that don't say which key
    // it was type through onInput.
    if (viaInput(e) || editKey(e)) return
    // Let the browser handle paste so the text can be sent.
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'v') return
    e.preventDefault()
    const printable = e.key.length === 1 && !e.ctrlKey && !e.metaKey
    send({ kind: 'keydown', key: e.key, code: e.code, keyCode: e.keyCode, text: printable ? e.key : e.key === 'Enter' ? '\r' : '', modifiers: mods(e) })
  }
  const onKeyUp = (e: React.KeyboardEvent) => {
    if (!control || viaInput(e) || editKey(e)) return
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'v') return
    e.preventDefault()
    send({ kind: 'keyup', key: e.key, code: e.code, keyCode: e.keyCode, modifiers: mods(e) })
  }
  const onPaste = (e: React.ClipboardEvent) => {
    if (!control) return
    e.preventDefault()
    const t = e.clipboardData.getData('text')
    if (t) type(t)
  }

  const onPointerDown = (e: React.PointerEvent) => {
    if (!control) return
    if (e.pointerType === 'touch') {
      // No mouse events after the tap: they'd take the focus from the
      // keyboard field and put the on-screen keyboard away.
      e.preventDefault()
      touch.current = { x: e.clientX, y: e.clientY, moved: false, typing: document.activeElement === keysRef.current }
      return
    }
    // Keep the keyboard on the page; the field takes it back right away.
    e.preventDefault()
    keysRef.current?.focus({ preventScroll: true })
    const p = toPage(e.clientX, e.clientY)
    send({ kind: 'mousedown', ...p, button: btn(e.button), clicks: e.detail || 1, modifiers: mods(e) })
  }
  const onPointerUp = (e: React.PointerEvent) => {
    if (!control) return
    const p = toPage(e.clientX, e.clientY)
    if (e.pointerType === 'touch') {
      const tc = touch.current
      touch.current = null
      if (tc && !tc.moved) {
        send({ kind: 'mousedown', ...p, button: 'left', clicks: 1 })
        send({ kind: 'mouseup', ...p, button: 'left', clicks: 1 })
        // Tapping the page keeps the on-screen keyboard up if it was.
        if (tc.typing) keysRef.current?.focus({ preventScroll: true })
      }
      return
    }
    send({ kind: 'mouseup', ...p, button: btn(e.button), clicks: e.detail || 1, modifiers: mods(e) })
  }
  const onPointerMove = (e: React.PointerEvent) => {
    if (!control) return
    if (e.pointerType === 'touch' && touch.current) {
      const dy = touch.current.y - e.clientY
      const dx = touch.current.x - e.clientX
      if (Math.abs(dy) + Math.abs(dx) > 6) {
        touch.current.moved = true
        const p = toPage(e.clientX, e.clientY)
        const r = boxRef.current!.getBoundingClientRect()
        const scale = size.w / r.width
        send({ kind: 'wheel', ...p, dx: dx * scale, dy: dy * scale })
        touch.current.x = e.clientX
        touch.current.y = e.clientY
      }
      return
    }
    const now = performance.now()
    if (now - lastMove.current < 40) return
    lastMove.current = now
    send({ kind: 'mousemove', ...toPage(e.clientX, e.clientY), modifiers: mods(e) })
  }
  const onWheel = (e: React.WheelEvent) => {
    if (!control) return
    send({ kind: 'wheel', ...toPage(e.clientX, e.clientY), dx: e.deltaX, dy: e.deltaY })
  }

  const host = (() => {
    try {
      const u = new URL(url)
      return u.host + (u.pathname !== '/' ? u.pathname : '') + u.search
    } catch {
      return url
    }
  })()

  return (
    <div className={`site${control ? ' control' : ''}`}>
      <div className="urlbar">
        {control && (
          <>
            <button type="button" className="nav-btn" aria-label="Back" onClick={() => send({ kind: 'back' })}>
              <Back size={16} />
            </button>
            <button type="button" className="nav-btn" aria-label="Forward" onClick={() => send({ kind: 'forward' })}>
              <Fwd size={16} />
            </button>
            <button type="button" className="nav-btn" aria-label="Reload" onClick={() => send({ kind: 'reload' })}>
              <Reload size={15} />
            </button>
          </>
        )}
        <div className="urlbox">
          <Lock size={11} />
          {control ? (
            <form
              style={{ flex: 1, display: 'flex', minWidth: 0 }}
              onSubmit={(e) => {
                e.preventDefault()
                if (editUrl) send({ kind: 'navigate', url: editUrl })
                setEditUrl(null)
                if (!coarse.current) keysRef.current?.focus({ preventScroll: true })
              }}
            >
              <label htmlFor="live-url" className="sr">
                Address
              </label>
              <input id="live-url" type="text" value={editUrl ?? url} onChange={(e) => setEditUrl(e.target.value)} onFocus={(e) => e.target.select()} onBlur={() => setEditUrl(null)} />
            </form>
          ) : (
            <span>{host || 'about:blank'}</span>
          )}
        </div>
        {control && coarse.current && (
          // Phones and tablets show their keyboard only for a text field.
          <button
            type="button"
            className="nav-btn"
            aria-label="Keyboard"
            aria-pressed={typing}
            onPointerDown={(e) => e.preventDefault()}
            onClick={() => {
              const k = keysRef.current
              if (typing) k?.blur()
              else k?.focus({ preventScroll: true })
            }}
          >
            <Keyboard size={17} />
          </button>
        )}
      </div>
      <div
        ref={boxRef}
        className={`screen${control ? ' control' : ''}`}
        style={{ aspectRatio: `${size.w} / ${size.h}` }}
        aria-label={control ? 'Shared browser — you are in control; type to type into the page' : 'Shared browser'}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerMove={onPointerMove}
        onWheel={onWheel}
        onContextMenu={(e) => control && e.preventDefault()}
      >
        {src ? <img src={src} alt="" draggable={false} /> : <div className="empty">{connected ? 'Opening the browser…' : 'Connecting to the browser…'}</div>}
        {control && (
          <textarea
            ref={keysRef}
            className="keys"
            aria-label="Type into the page"
            defaultValue={pad}
            autoCapitalize="none"
            autoCorrect="off"
            autoComplete="off"
            spellCheck={false}
            onFocus={() => {
              resetKeys()
              setTyping(true)
            }}
            onBlur={() => setTyping(false)}
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
        {!control && cursor && (
          <span ref={pointerRef} className="cursor">
            <span className="rip" />
            <CursorArrow />
          </span>
        )}
      </div>
    </div>
  )
}
