import type { Img, Tap, Via } from './blocks'
import type {
  AppNotification,
  Attachment,
  Entry,
  FeedPage,
  FileInfo,
  FormValues,
  Login,
  Memory,
  Report,
  Schedule,
  SendResult,
  ServerConfig,
  SessionData,
  SessionState,
  Surface,
  User,
} from './types'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

// A 401 on any request means this session is gone (signed out, revoked or
// expired). The app routes back to sign-in.
let unauthorized: (() => void) | null = null
export function onUnauthorized(fn: (() => void) | null) {
  unauthorized = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, headers: { 'X-Alexa': '1' }, credentials: 'same-origin' }
  if (body instanceof FormData) {
    init.body = body
  } else if (body !== undefined) {
    ;(init.headers as Record<string, string>)['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  const res = await fetch(path, init)
  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = null
  }
  if (!res.ok) {
    const e = (data as { error?: unknown } | null)?.error
    const msg = typeof e === 'string' && e ? e : res.statusText || `HTTP ${res.status}`
    if (res.status === 401) unauthorized?.()
    throw new ApiError(res.status, msg)
  }
  return data as T
}

// newClientId makes the id a message is sent with, so a retry or the
// socket's echo is recognised as the same message (WP-18).
export function newClientId() {
  try {
    return crypto.randomUUID()
  } catch {
    return `c${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`
  }
}

const enc = encodeURIComponent

export const api = {
  setup: () => request<{ needsSetup: boolean; signupAllowed: boolean }>('GET', '/api/setup'),
  signup: (b: { username: string; password: string; displayName?: string; timezone?: string }) => request<User>('POST', '/api/auth/signup', b),
  login: (b: { username: string; password: string; timezone?: string }) => request<User>('POST', '/api/auth/login', b),
  logout: () => request('POST', '/api/auth/logout'),
  me: () => request<User>('GET', '/api/me'),
  updateMe: (b: Partial<Pick<User, 'displayName' | 'email' | 'timezone' | 'about'>>) => request<User>('PUT', '/api/me', b),
  changePassword: (current: string, next: string) => request('PUT', '/api/me/password', { current, new: next }),
  config: () => request<ServerConfig>('GET', '/api/config'),

  session: () => request<SessionData>('GET', '/api/session'),
  feed: (before: number) => request<FeedPage>('GET', `/api/feed?before=${before}`),
  // send posts a visible user message. A server with WP-18 answers with the
  // stored entry, which is returned; an older one answers {ok:true}. extra
  // says how the app made the message when the user didn't type it: a
  // tapped block action, a submitted form, a multi-select or a report.
  send: async (text: string, attachments: Attachment[], voice: boolean, clientId?: string, extra?: SendExtra): Promise<Entry | null> => {
    const r = await request<SendResult | null>('POST', '/api/messages', { text, attachments, voice, clientId, ...extra })
    return r?.entry ?? null
  },
  // interrupt stops the current step and sends text instead (WP-14). It
  // takes the same body as send and answers the same way.
  interrupt: async (text: string, clientId?: string, attachments: Attachment[] = [], voice = false): Promise<Entry | null> => {
    const r = await request<SendResult | null>('POST', '/api/interrupt', { text, attachments, voice, clientId })
    return r?.entry ?? null
  },
  // retry relaunches a failed run from its last good item (WP-15).
  retry: () => request('POST', '/api/retry'),
  // repair sets aside inputs the model can't read (WP-17), and says which.
  repair: () => request<{ ok: boolean; setAside?: string[] }>('POST', '/api/repair'),
  takeOver: () => request('POST', '/api/takeover'),
  signIn: (username: string, password: string, save: boolean) => request('POST', '/api/signin', { username, password, save }),
  handBack: (note = '') => request('POST', '/api/handback', { note }),
  stop: () => request('POST', '/api/stop'),
  setSurface: (s: Surface) => request('PUT', '/api/surface', s),
  compact: () => request<SessionState>('POST', '/api/session/compact'),
  reset: () => request('POST', '/api/session/reset'),

  reports: () => request<Report[]>('GET', '/api/reports'),
  report: (id: string) => request<Report>('GET', `/api/reports/${enc(id)}`),
  deleteReport: (id: string) => request('DELETE', `/api/reports/${enc(id)}`),
  reportDownloadURL: (id: string) => `/api/reports/${enc(id)}/download`,
  reportCalendarURL: (id: string) => `/api/reports/${enc(id)}/calendar.ics`,
  // An entry's timeline blocks as a calendar file (WP-03).
  entryCalendarURL: (id: number) => `/api/entries/${id}/calendar.ics`,

  // upload answers with the attachment and, when Alexa can't look at it as
  // a picture, a note for the person (WP-17).
  upload: (file: File) => {
    const fd = new FormData()
    fd.append('file', file)
    return request<Attachment & { note?: string }>('POST', '/api/uploads', fd)
  },
  files: () => request<FileInfo[]>('GET', '/api/files'),
  deleteFile: (path: string) => request('DELETE', `/api/files?path=${enc(path)}`),
  fileURL: (path: string, inline = false) => `/api/files/download?path=${enc(path)}${inline ? '&inline=1' : ''}`,

  memories: () => request<Memory[]>('GET', '/api/memories'),
  addMemory: (content: string) => request<Memory>('POST', '/api/memories', { content }),
  updateMemory: (id: number, content: string) => request('PUT', `/api/memories/${id}`, { content }),
  deleteMemory: (id: number) => request('DELETE', `/api/memories/${id}`),

  schedules: () => request<Schedule[]>('GET', '/api/schedules'),
  createSchedule: (b: { name: string; prompt: string; cron?: string; runAt?: number }) => request<Schedule>('POST', '/api/schedules', b),
  setScheduleEnabled: (id: string, enabled: boolean) => request('PATCH', `/api/schedules/${id}`, { enabled }),
  deleteSchedule: (id: string) => request('DELETE', `/api/schedules/${id}`),
  runSchedule: (id: string) => request('POST', `/api/schedules/${id}/run`),

  logins: () => request<Login[]>('GET', '/api/logins'),
  addLogin: (b: { site: string; username: string; password: string }) => request<Login>('POST', '/api/logins', b),
  updateLogin: (id: number, b: { username?: string; password?: string }) => request<Login>('PUT', `/api/logins/${id}`, b),
  deleteLogin: (id: number) => request('DELETE', `/api/logins/${id}`),

  notifications: () => request<AppNotification[]>('GET', '/api/notifications'),
  markRead: () => request('POST', '/api/notifications/read'),
  pushSubscribe: (sub: PushSubscriptionJSON) => request('POST', '/api/push/subscribe', sub),

  users: () => request<User[]>('GET', '/api/admin/users'),
  createUser: (b: { username: string; password: string; displayName?: string; isAdmin?: boolean }) => request<User>('POST', '/api/admin/users', b),
  deleteUser: (id: number) => request('DELETE', `/api/admin/users/${id}`),
  resetPassword: (id: number, password: string) => request('PUT', `/api/admin/users/${id}/password`, { password }),

  transcribe: async (wav: Blob) => {
    const res = await fetch('/api/transcribe', { method: 'POST', body: wav, headers: { 'X-Alexa': '1', 'Content-Type': 'audio/wav' } })
    if (!res.ok) throw new ApiError(res.status, 'transcription failed')
    return ((await res.json()) as { text: string }).text
  },

  // Media (WP-03/11). Favicons and remote images only ever come through the
  // server, which fetches public addresses only and caches them.
  faviconURL: (host: string) => `/api/favicon/${enc(host.replace(/^www\./, ''))}`,
  // imgURL turns a block image into something an <img> can load: the
  // server's minted /api/img/{token} URLs pass through; a workspace path is
  // served from the workspace.
  imgURL: (src: Img) => (src.startsWith('/api/') || /^https:\/\//i.test(src) || src.startsWith('data:') ? src : `/api/files/download?path=${enc(src)}&inline=1`),
}

// askEntry is the entry of a question the user skipped and is answering
// late, so the server reads the answer against that question's form and
// not the one open now (FE-1).
export type SendExtra = { via?: Via; tap?: Tap; values?: FormValues; askEntry?: number }

export function wsURL(path: string) {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}${path}`
}
