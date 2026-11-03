// The API's data types: what the Go server sends and takes. This file is the
// contract both sides match. Changes are additive only: new fields are
// optional, so an older server (or an older client) keeps working.
// Envelope and entry fields are camelCase; block fields (api/blocks.ts) are
// snake_case because the model writes them.

import type { AskForm, Block, ReportPreview, Source, Tap, Via } from './blocks'
import type { Failure, Plan, RunPhase, StepData } from './progress'

export type User = {
  id: number
  username: string
  displayName: string
  email: string
  timezone: string
  about: string
  isAdmin: boolean
  createdAt: number
}

export type Attachment = { name: string; path: string; mime: string; size: number }

// Needs is what Alexa is waiting on the user for.
export type Needs = {
  kind: 'takeover' | 'question' | 'login'
  message: string
  options?: string[]
  site?: string
  username?: string
  entryId?: number
  // WP-03: the rich ask (Appendix A.4).
  blocks?: Block[]
  form?: AskForm
  multi?: boolean
  recommended?: number // index into options
  confirm?: boolean
}

// Surface is what the screen shows next to the exchange: nothing, the live
// browser, a report, or one entry's blocks expanded (WP-04, Appendix C.1).
export type SurfaceKind = '' | 'browser' | 'report' | 'entry'
// held is set while the user's own choice keeps the screen where it is (R1);
// follow, sent with PUT /api/surface, turns following Alexa back on.
export type Surface = { kind: SurfaceKind; id?: string; held?: boolean; follow?: boolean }

export type SessionStatus = 'idle' | 'working' | 'needs'

// The session: Alexa's one never-ending conversation with this user. The
// server calls it the View.
export type SessionState = {
  status: SessionStatus
  activity: string
  needs?: Needs | null
  surface: Surface
  control: boolean
  browserUrl?: string
  contextTokens: number
  compactions: number
  // WP-05/07/18 (Appendix B.3, B.5).
  phase?: RunPhase
  runStartedAt?: number // unix ms
  plan?: Plan | null
  follow?: boolean // follow mode: the agent's browsing opens the browser panel
  queued?: number // messages waiting for Alexa to read them
  seq?: number
  epoch?: string
}
export type View = SessionState

export type EntryKind = 'user' | 'say' | 'ask' | 'takeover' | 'login' | 'report' | 'step' | 'error' | 'note' | 'plan'

// step: running|done|failed|skipped|interrupted; ask, takeover, login:
// open|answered.
export type EntryStatus = 'running' | 'done' | 'failed' | 'skipped' | 'interrupted' | 'open' | 'answered'

export type EntryData = {
  attachments?: Attachment[]
  voice?: boolean
  options?: string[]
  id?: string // report entries: the report's id
  updated?: boolean
  // start marks the message that set Alexa to work while it was idle: the
  // first entry of an exchange.
  start?: boolean
  // followUp says the exchange carries on the topic before it; topic is
  // "pending" while the server is still deciding.
  followUp?: boolean
  topic?: 'pending'
  scheduled?: boolean
  site?: string
  username?: string
  // WP-03: generative UI (Appendix A).
  blocks?: Block[]
  sources?: Source[]
  suggestions?: string[]
  final?: boolean // this say or ask ends Alexa's work on the request
  preview?: ReportPreview // report entries
  takeaway?: string // report entries: the line shown on the card
  // summary is the server's text for the blocks (≤160), on say and ask
  // entries: what push, voice and search see of them.
  summary?: string
  form?: AskForm // ask entries
  multi?: boolean
  recommended?: number
  confirm?: boolean
  // WP-03/10/18: how a visible user reply was made, and the id the client
  // sent it with (the echo carries it back for dedupe).
  via?: Via | 'retry' | 'handback'
  clientId?: string
  tap?: Tap // via button: which block action was tapped
  values?: FormValues // via form: what was submitted, by field name
  retry?: number // via retry: the error entry tried again
  // WP-05/15 (Appendix B).
  plan?: Plan // plan entries
  step?: StepData // step entries
  failure?: Failure // error entries
}

// Entry is one item of the conversation as the user sees it.
export type Entry = {
  id: number
  kind: EntryKind | (string & {}) // unknown kinds render as their text
  text: string
  doneText?: string
  status?: EntryStatus | (string & {})
  data?: EntryData
  createdAt: number
  updatedAt: number
  // WP-18: bumped on every update; the reducer ignores older revisions.
  rev?: number
}

export type ReportItem = { label: string; detail?: string }
export type CalEvent = { title: string; start: string; end?: string; allDay?: boolean; location?: string; description?: string }
export type Link = { title: string; url: string }
export type ReportData = {
  items?: ReportItem[]
  events?: CalEvent[]
  files?: string[]
  sources?: Link[]
  // WP-03/09.
  blocks?: Block[]
  takeaway?: string // ≤200, shown on the report card
  preview?: ReportPreview
}
// Report is a report as GET /api/reports/{id} sends it. The list (GET
// /api/reports) sends what a card previews (home.ts preview): the start of
// a markdown report's content and, of its data, the takeaway, the preview
// and the first item (D4).
export type Report = {
  id: string
  title: string
  format: 'markdown' | 'html'
  content?: string
  data?: ReportData
  createdAt: number
  updatedAt: number
}

// SessionData is the snapshot GET /api/session returns. seq and epoch
// (WP-18) say which event the snapshot is current to.
export type SessionData = { state: SessionState; feed: Entry[]; hasMore: boolean; report?: Report; seq?: number; epoch?: string }
export type FeedPage = { feed: Entry[]; hasMore: boolean }

// source says who wrote a memory: the user, Alexa as they talked, or Alexa
// looking back over a conversation afterwards.
export type Memory = { id: number; content: string; source?: 'user' | 'alexa' | 'review'; createdAt: number; updatedAt?: number }
export type Schedule = {
  id: string
  name: string
  cron?: string
  runAt?: number
  prompt: string
  enabled: boolean
  lastRun?: number
  nextRun?: number
  createdAt: number
}
// AppNotification is one of the user's notifications (named so it doesn't
// shadow the DOM's Notification).
export type AppNotification = { id: number; ref?: string; title: string; body: string; read: boolean; createdAt: number }
// A saved login. Its password stays on the server.
export type Login = { id: number; site: string; username: string; createdAt: number; updatedAt: number; usedAt?: number }
export type FileInfo = { path: string; size: number; modTime: number; isDir: boolean }
export type ServerConfig = {
  model: string
  viewport: { width: number; height: number }
  emailEnabled: boolean
  shellEnabled: boolean
  sandboxed: boolean
  vapidPublicKey: string
}

// SendResult is what POST /api/messages answers: today {ok:true}; with
// WP-18 also the stored entry.
export type SendResult = { ok?: boolean; entry?: Entry }

// What a submitted form sends, by field name.
export type FormValues = Record<string, string | number | boolean | string[]>
