import { useEffect, useRef } from 'react'
import { wsURL } from './api'

export type ServerEvent = { type: string; data?: unknown }

// useEvents keeps a WebSocket to /api/ws open (reconnecting with backoff)
// and calls onEvent for each message. onReconnect fires after a reconnect so
// the caller can resync state it may have missed. The socket also tells the
// server whether this page is on screen: while it isn't, Alexa's messages
// arrive as notifications.
export function useEvents(enabled: boolean, onEvent: (e: ServerEvent) => void, onReconnect: () => void) {
  const handler = useRef(onEvent)
  const reconnect = useRef(onReconnect)
  handler.current = onEvent
  reconnect.current = onReconnect

  useEffect(() => {
    if (!enabled) return
    let ws: WebSocket | null = null
    let stopped = false
    let attempt = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    let first = true
    const visible = () => document.visibilityState === 'visible'
    const report = () => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'visibility', visible: visible() }))
    }
    const connect = () => {
      ws = new WebSocket(wsURL('/api/ws'))
      ws.onopen = () => {
        attempt = 0
        report()
        if (!first) reconnect.current()
        first = false
      }
      ws.onmessage = (ev) => {
        try {
          handler.current(JSON.parse(ev.data))
        } catch {
          /* ignore */
        }
      }
      ws.onclose = () => {
        if (stopped) return
        attempt++
        timer = setTimeout(connect, Math.min(15000, 500 * 2 ** attempt))
      }
    }
    connect()
    const onVis = () => {
      if (visible() && ws && ws.readyState > 1) {
        clearTimeout(timer)
        connect()
        return
      }
      report()
    }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      stopped = true
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVis)
      ws?.close()
    }
  }, [enabled])
}
