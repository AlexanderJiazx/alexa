import type { BrowserEvent } from '../api/realtime'
import { createStore, useSelector } from './store'

// The fast-moving bits live in stores of their own, so a pointer move or a
// word of a voice transcript re-renders only what shows it (FE-06), never
// the conversation.

// ---------- the shared browser: where the page is, where Alexa points ----------

export type Cursor = { x: number; y: number; click: boolean; seq: number }
// crashed counts the times the browser died under the live view (B02): the
// view shows Reconnecting at once, before its own socket notices.
export type Live = { url: string; title: string; cursor: Cursor | null; crashed?: number }

export const liveStore = createStore<Live>({ url: '', title: '', cursor: null })

// applyBrowser applies a browser event from the socket.
export function applyBrowser(d: BrowserEvent) {
  if (d.type === 'nav') liveStore.set((l) => (d.url && d.url !== l.url ? { ...l, url: d.url, title: d.title ?? l.title } : l))
  if (d.type === 'cursor') liveStore.set((l) => ({ ...l, cursor: { x: d.x || 0, y: d.y || 0, click: !!d.click, seq: (l.cursor?.seq || 0) + 1 } }))
  if (d.type === 'crashed') liveStore.set((l) => ({ ...l, crashed: (l.crashed ?? 0) + 1 }))
}

// setLiveUrl records where the page is, as the live view's hello says.
export function setLiveUrl(url: string, title?: string) {
  liveStore.set((l) => (url === l.url && (title === undefined || title === l.title) ? l : { ...l, url, title: title ?? l.title }))
}

export function useLive<T>(select: (l: Live) => T): T {
  return useSelector(liveStore, select)
}

// ---------- what the microphone heard so far ----------

export type Heard = { committed: string; partial: string }

export const heardStore = createStore<Heard>({ committed: '', partial: '' })

export function setHeard(committed: string, partial: string) {
  heardStore.set((h) => (h.committed === committed && h.partial === partial ? h : { committed, partial }))
}

const all = (h: Heard) => h
export function useHeard(): Heard {
  return useSelector(heardStore, all)
}
