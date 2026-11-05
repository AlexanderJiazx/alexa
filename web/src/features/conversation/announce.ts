import type { Entry } from '../../api/types'
import type { ServerEvent } from '../../api/realtime'
import { announce } from '../../ui/LiveAnnouncer'
import { entryKinds } from './registry'

// What Alexa needs from the user is read out at once; the rest politely.
const urgent = new Set(['ask', 'takeover', 'login'])

// announcer says what a screen reader should hear when an entry arrives
// (FE-08): each entry once, the first time it comes in live, never the
// updates that follow or the entries a snapshot brings back.
export function announcer() {
  const heard = new Set<number>()
  return (ev: ServerEvent) => {
    if (ev.type !== 'entry' || !ev.data) return
    const e = ev.data as Entry
    if (heard.has(e.id)) return
    heard.add(e.id)
    const text = entryKinds[e.kind]?.announce?.(e)
    if (text) announce(text, { assertive: urgent.has(e.kind) })
  }
}
