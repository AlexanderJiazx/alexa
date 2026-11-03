// The realtime protocol over /api/ws (SYNTHESIS Appendix B.5). Every server
// message is an envelope {type, data?, seq?, epoch?}. seq is one counter per
// user across every event type (browser events included) and epoch changes
// when the server restarts; an older server sends neither, and the client
// then falls back to resyncing on every connect.

import type { RunData } from './progress'
import type { AppNotification, Entry, Report, SessionState } from './types'

export type Envelope<T extends string = string, D = unknown> = { type: T; data?: D; seq?: number; epoch?: string }

// The browser events the live view needs: where the page is and where
// Alexa's pointer is going.
export type BrowserEvent = { type: 'nav' | 'cursor' | (string & {}); url?: string; title?: string; x?: number; y?: number; click?: boolean }

// What changed on the server outside the conversation (WP-18), so open
// sheets can refetch: memory, schedules, logins, files, me, reportDeleted.
export type ChangedWhat = 'memory' | 'schedules' | 'logins' | 'files' | 'me' | 'reportDeleted' | 'reports' | 'notifications'
export type Changed = { what: ChangedWhat; id?: string | number }

// hello is the first message on every socket, sent once the server has
// subscribed this client. With WP-18 it says which epoch and seq the server
// is at.
export type Hello = { seq?: number; epoch?: string }

export type ServerEvent =
  | (Envelope<'hello', Hello> & { seq?: number; epoch?: string })
  | Envelope<'state', SessionState>
  | Envelope<'entry', Entry>
  | Envelope<'entryStatus', { id: number; status: string }>
  | Envelope<'report', Report>
  | Envelope<'browser', BrowserEvent>
  | Envelope<'notification', AppNotification>
  | Envelope<'reset'>
  | Envelope<'resync'>
  | Envelope<'run', RunData>
  | Envelope<'changed', Changed>

// What the client sends. attention is WP-18's heartbeat; visibility is what
// the current server understands, so the client sends both.
export type ClientMessage = { type: 'attention'; visible: boolean; focused: boolean } | { type: 'visibility'; visible: boolean }

// The close code a server uses when this session was revoked (logout on
// another device, password change, user deleted): back to sign-in.
export const closeRevoked = 4401

// The close code a server uses when this client fell too far behind: it
// reconnects at once with its cursor, and what it missed is replayed.
export const closeLagged = 4000

// afterParam is how a reconnecting client says what it has already seen:
// ?after=<epoch>:<seq>.
export function afterParam(epoch?: string, seq?: number) {
  if (!epoch || seq === undefined || seq === null) return ''
  return `${epoch}:${seq}`
}

export function eventsPath(after: string) {
  return after ? `/api/ws?after=${encodeURIComponent(after)}` : '/api/ws'
}
