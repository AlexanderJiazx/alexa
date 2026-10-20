import { useEffect, useRef, useState } from 'react'
import { wsURL } from '../lib/api'
import { Back, CursorArrow, Fwd, Lock, Reload } from './Icons'

export type Cursor = { x: number; y: number; click: boolean; seq: number }

type Props = {
  control: boolean
  cursor: Cursor | null
  url: string
  onUrl: (url: string, title?: string) => void
}

// LiveScreen streams the session's shared browser as JPEG frames and, when
// the user is in control, forwards mouse and keyboard input to it.
export function LiveScreen({ control, cursor, url, onUrl }: Props) {
  const [src, setSrc] = useState<string | null>(null)
  const [size, setSize] = useState({ w: 1280, h: 800 })
  const [connected, setConnected] = useState(false)
  const [editUrl, setEditUrl] = useState<string | null>(null)
  const [typeText, setTypeText] = useState('')
  const wsRef = useRef<WebSocket | null>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const lastMove = useRef(0)
  const touch = useRef<{ x: number; y: number; moved: boolean } | null>(null)
  const onUrlRef = useRef(onUrl)
  onUrlRef.current = onUrl

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

  useEffect(() => {
    if (control) boxRef.current?.focus()
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

  const onPointerDown = (e: React.PointerEvent) => {
    if (!control) return
    boxRef.current?.focus()
    if (e.pointerType === 'touch') {
      touch.current = { x: e.clientX, y: e.clientY, moved: false }
      return
    }
    const p = toPage(e.clientX, e.clientY)
    send({ kind: 'mousedown', ...p, button: btn(e.button), clicks: e.detail || 1, modifiers: mods(e) })
  }
  const onPointerUp = (e: React.PointerEvent) => {
    if (!control) return
    const p = toPage(e.clientX, e.clientY)
    if (e.pointerType === 'touch') {
      if (touch.current && !touch.current.moved) {
        send({ kind: 'mousedown', ...p, button: 'left', clicks: 1 })
        send({ kind: 'mouseup', ...p, button: 'left', clicks: 1 })
      }
      touch.current = null
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
  const onKey = (e: React.KeyboardEvent, down: boolean) => {
    if (!control) return
    // Let the browser handle paste so we can forward the text.
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'v') return
    e.preventDefault()
    const printable = e.key.length === 1 && !e.ctrlKey && !e.metaKey
    send({ kind: down ? 'keydown' : 'keyup', key: e.key, code: e.code, keyCode: e.keyCode, text: down && printable ? e.key : down && e.key === 'Enter' ? '\r' : '', modifiers: mods(e) })
  }
  const onPaste = (e: React.ClipboardEvent) => {
    if (!control) return
    e.preventDefault()
    const t = e.clipboardData.getData('text')
    if (t) send({ kind: 'text', text: t })
  }

  const scale = boxRef.current ? boxRef.current.getBoundingClientRect().width / size.w : 0
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
                boxRef.current?.focus()
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
      </div>
      <div
        ref={boxRef}
        className={`screen${control ? ' control' : ''}`}
        style={{ aspectRatio: `${size.w} / ${size.h}` }}
        tabIndex={control ? 0 : -1}
        aria-label={control ? 'Shared browser — you are in control' : 'Shared browser'}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerMove={onPointerMove}
        onWheel={onWheel}
        onKeyDown={(e) => onKey(e, true)}
        onKeyUp={(e) => onKey(e, false)}
        onPaste={onPaste}
        onContextMenu={(e) => control && e.preventDefault()}
      >
        {src ? <img src={src} alt="" draggable={false} /> : <div className="empty">{connected ? 'Opening the browser…' : 'Connecting to the browser…'}</div>}
        {!control && cursor && scale > 0 && (
          <span key={cursor.seq} className="cursor" style={{ left: cursor.x * scale, top: cursor.y * scale }}>
            {cursor.click && <span className="rip" />}
            <CursorArrow />
          </span>
        )}
      </div>
      {control && (
        <form
          className="urlbar"
          style={{ borderTop: '1px solid var(--line)', borderBottom: 0, justifyContent: 'stretch', height: 44 }}
          onSubmit={(e) => {
            e.preventDefault()
            if (typeText) send({ kind: 'text', text: typeText })
            send({ kind: 'keydown', key: 'Enter', code: 'Enter', keyCode: 13, text: '\r' })
            send({ kind: 'keyup', key: 'Enter', code: 'Enter', keyCode: 13 })
            setTypeText('')
          }}
        >
          <div className="urlbox" style={{ width: '100%', maxWidth: 'none' }}>
            <label htmlFor="live-type" className="sr">
              Type into the page
            </label>
            <input
              id="live-type"
              type="text"
              placeholder="Type into the page (or click the page and use your keyboard)"
              value={typeText}
              onChange={(e) => setTypeText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Tab' && typeText) {
                  e.preventDefault()
                  send({ kind: 'text', text: typeText })
                  setTypeText('')
                }
              }}
              onBlur={() => {
                if (typeText) {
                  send({ kind: 'text', text: typeText })
                  setTypeText('')
                }
              }}
            />
          </div>
        </form>
      )}
    </div>
  )
}
