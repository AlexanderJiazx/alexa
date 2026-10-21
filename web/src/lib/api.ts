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

export type Needs = { kind: 'takeover' | 'question' | 'login'; message: string; options?: string[]; site?: string; username?: string; entryId?: number }
export type Surface = { kind: '' | 'browser' | 'report'; id?: string }

// The session: Alexa's one never-ending conversation with this user.
export type SessionState = {
  status: 'idle' | 'working' | 'needs'
  activity: string
  needs?: Needs | null
  surface: Surface
  control: boolean
  browserUrl?: string
  contextTokens: number
  compactions: number
}

// Entry is one item of the conversation as the user sees it.
export type Entry = {
  id: number
  kind: 'user' | 'say' | 'ask' | 'takeover' | 'login' | 'report' | 'step' | 'error' | 'note'
  text: string
  doneText?: string
  status?: string
  data?: {
    attachments?: Attachment[]
    voice?: boolean
    options?: string[]
    id?: string
    updated?: boolean
    // start marks the message that set Alexa to work while it was idle: the
    // first entry of an exchange.
    start?: boolean
    scheduled?: boolean
    site?: string
    username?: string
  }
  createdAt: number
  updatedAt: number
}

export type ReportItem = { label: string; detail?: string }
export type CalEvent = { title: string; start: string; end?: string; allDay?: boolean; location?: string; description?: string }
export type Link = { title: string; url: string }
export type ReportData = { items?: ReportItem[]; events?: CalEvent[]; files?: string[]; sources?: Link[] }
export type Report = {
  id: string
  title: string
  format: 'markdown' | 'html'
  content?: string
  data?: ReportData
  createdAt: number
  updatedAt: number
}

export type SessionData = { state: SessionState; feed: Entry[]; hasMore: boolean; report?: Report }

export type Memory = { id: number; content: string; createdAt: number }
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
export type Notification = { id: number; ref?: string; title: string; body: string; read: boolean; createdAt: number }
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

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
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
    throw new ApiError(res.status, msg)
  }
  return data as T
}

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
  feed: (before: number) => request<{ feed: Entry[]; hasMore: boolean }>('GET', `/api/feed?before=${before}`),
  send: (text: string, attachments: Attachment[], voice: boolean) => request('POST', '/api/messages', { text, attachments, voice }),
  takeOver: () => request('POST', '/api/takeover'),
  signIn: (username: string, password: string, save: boolean) => request('POST', '/api/signin', { username, password, save }),
  handBack: (note = '') => request('POST', '/api/handback', { note }),
  stop: () => request('POST', '/api/stop'),
  setSurface: (s: Surface) => request('PUT', '/api/surface', s),
  compact: () => request<SessionState>('POST', '/api/session/compact'),
  reset: () => request('POST', '/api/session/reset'),

  reports: () => request<Report[]>('GET', '/api/reports'),
  report: (id: string) => request<Report>('GET', `/api/reports/${encodeURIComponent(id)}`),
  deleteReport: (id: string) => request('DELETE', `/api/reports/${encodeURIComponent(id)}`),
  reportDownloadURL: (id: string) => `/api/reports/${encodeURIComponent(id)}/download`,
  reportCalendarURL: (id: string) => `/api/reports/${encodeURIComponent(id)}/calendar.ics`,

  upload: (file: File) => {
    const fd = new FormData()
    fd.append('file', file)
    return request<Attachment>('POST', '/api/uploads', fd)
  },
  files: () => request<FileInfo[]>('GET', '/api/files'),
  deleteFile: (path: string) => request('DELETE', `/api/files?path=${encodeURIComponent(path)}`),
  fileURL: (path: string, inline = false) => `/api/files/download?path=${encodeURIComponent(path)}${inline ? '&inline=1' : ''}`,

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

  notifications: () => request<Notification[]>('GET', '/api/notifications'),
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
}

export function wsURL(path: string) {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}${path}`
}

export function elapsed(from: number, to: number) {
  const s = Math.max(0, Math.round((to - from) / 1000))
  if (s >= 3600) return `${Math.floor(s / 3600)}:${String(Math.floor((s % 3600) / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

export function ago(ts: number) {
  const s = Math.round((Date.now() - ts) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  const d = new Date(ts)
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

export function fmtSize(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export function clock(ts: number) {
  return new Date(ts).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

// day labels a timestamp for the conversation's time dividers.
export function day(ts: number) {
  const d = new Date(ts)
  const today = new Date()
  const yesterday = new Date(Date.now() - 864e5)
  if (d.toDateString() === today.toDateString()) return 'Today'
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday'
  return d.toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric', year: d.getFullYear() === today.getFullYear() ? undefined : 'numeric' })
}

// siteAddress turns what someone typed for a site into the form browsers
// use, with internationalised names in ASCII (punycode).
export function siteAddress(s: string) {
  const t = s.trim()
  try {
    return new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(t) ? t : `https://${t}`).hostname || t
  } catch {
    return t
  }
}

export function hostOf(u: string) {
  try {
    return new URL(u).host.replace(/^www\./, '')
  } catch {
    return u
  }
}
