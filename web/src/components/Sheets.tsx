import { useEffect, useState, type ReactNode } from 'react'
import type { Login, Memory, Notification, Schedule, ServerConfig, SessionState, User } from '../lib/api'
import { ago, api, siteAddress } from '../lib/api'
import { getTheme, setTheme, type Theme } from '../lib/theme'
import { Bell, Brain, Clock, Close, Gear, Globe, History, KeyIcon, Out, People, Play, Trash } from './Icons'

export type SheetName = 'schedules' | 'memory' | 'logins' | 'notifications' | 'settings' | 'users'

// ---------- account menu ----------

export function AccountMenu({ user, unread, onOpen, onReflect, onBrowser, onSignOut, onClose }: { user: User; unread: number; onOpen: (s: SheetName) => void; onReflect: () => void; onBrowser: () => void; onSignOut: () => void; onClose: () => void }) {
  const item = (s: SheetName, icon: ReactNode, label: string, count?: number) => (
    <button type="button" role="menuitem" onClick={() => onOpen(s)}>
      {icon}
      <span>{label}</span>
      {count ? <span className="count">{count}</span> : null}
    </button>
  )
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <div className="menu" role="menu" aria-label="Account">
        <div className="who">
          <b>{user.displayName || user.username}</b>@{user.username}
        </div>
        <button type="button" role="menuitem" onClick={onReflect}>
          <History size={18} />
          <span>Reflect</span>
        </button>
        <button type="button" role="menuitem" onClick={onBrowser}>
          <Globe size={18} />
          <span>Browser</span>
        </button>
        {item('schedules', <Clock size={18} />, 'Scheduled')}
        {item('memory', <Brain size={18} />, 'Memory')}
        {item('logins', <KeyIcon size={18} />, 'Logins')}
        {item('notifications', <Bell size={18} />, 'Notifications', unread)}
        {item('settings', <Gear size={18} />, 'Settings')}
        {user.isAdmin && item('users', <People size={18} />, 'Users')}
        <button type="button" role="menuitem" onClick={onSignOut}>
          <Out size={18} />
          <span>Sign out</span>
        </button>
      </div>
    </>
  )
}

function Sheet({ title, onClose, children, actions }: { title: string; onClose: () => void; children: ReactNode; actions?: ReactNode }) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [onClose])
  return (
    <>
      <div className="scrim dim" onClick={onClose} />
      <section className="sheet" role="dialog" aria-label={title}>
        <div className="sheet-head">
          <h2>{title}</h2>
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            {actions}
            <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>
              <Close size={18} />
            </button>
          </div>
        </div>
        <div className="sheet-body">{children}</div>
      </section>
    </>
  )
}

// ---------- schedules ----------

function describeCron(c: string) {
  const p = c.trim().split(/\s+/)
  if (p.length !== 5) return c
  const [m, h, dom, mon, dow] = p
  const time = /^\d+$/.test(m) && /^\d+$/.test(h) ? `${h.padStart(2, '0')}:${m.padStart(2, '0')}` : null
  const days: Record<string, string> = { '1-5': 'weekdays', '0,6': 'weekends', '6,0': 'weekends', '*': 'every day', '0': 'Sundays', '1': 'Mondays', '2': 'Tuesdays', '3': 'Wednesdays', '4': 'Thursdays', '5': 'Fridays', '6': 'Saturdays', '7': 'Sundays' }
  if (time && dom === '*' && mon === '*' && days[dow]) return `${days[dow][0].toUpperCase()}${days[dow].slice(1)} at ${time}`
  if (time && /^\d+$/.test(dom) && mon === '*' && dow === '*') return `Monthly on day ${dom} at ${time}`
  if (m.startsWith('*/') && h === '*') return `Every ${m.slice(2)} minutes`
  if (/^\d+$/.test(m) && h.startsWith('*/')) return `Every ${h.slice(2)} hours`
  return c
}

function fmtWhen(ts?: number) {
  if (!ts) return ''
  return new Date(ts).toLocaleString(undefined, { weekday: 'short', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

export function SchedulesSheet({ onClose }: { onClose: () => void }) {
  const [list, setList] = useState<Schedule[] | null>(null)
  const [adding, setAdding] = useState(false)
  const [form, setForm] = useState({ name: '', prompt: '', cron: '0 8 * * *' })
  const [err, setErr] = useState('')
  const load = () => api.schedules().then(setList).catch((e) => setErr(e.message))
  useEffect(() => {
    load()
  }, [])
  return (
    <Sheet
      title="Scheduled"
      onClose={onClose}
      actions={
        !adding && (
          <button type="button" className="btn xs" onClick={() => setAdding(true)}>
            New
          </button>
        )
      }
    >
      {adding && (
        <form
          className="sec"
          onSubmit={async (e) => {
            e.preventDefault()
            setErr('')
            try {
              await api.createSchedule(form)
              setAdding(false)
              setForm({ name: '', prompt: '', cron: '0 8 * * *' })
              load()
            } catch (e) {
              setErr((e as Error).message)
            }
          }}
        >
          <div className="field">
            <label htmlFor="sc-prompt">What should Alexa do?</label>
            <textarea id="sc-prompt" required value={form.prompt} onChange={(e) => setForm({ ...form, prompt: e.target.value })} placeholder="Check my favourite news sites and send me a 5-bullet briefing" />
          </div>
          <div className="inline">
            <div className="field">
              <label htmlFor="sc-name">Name</label>
              <input id="sc-name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Morning briefing" />
            </div>
            <div className="field">
              <label htmlFor="sc-cron">When (cron)</label>
              <input id="sc-cron" required value={form.cron} onChange={(e) => setForm({ ...form, cron: e.target.value })} />
            </div>
          </div>
          <p className="field help" style={{ marginTop: 8 }}>
            {describeCron(form.cron)} in your timezone. Tip: you can also just ask Alexa, e.g. “every weekday at 8, summarise my inbox”.
          </p>
          <div className="row-btns" style={{ justifyContent: 'flex-start', marginTop: 12 }}>
            <button type="submit" className="btn primary sm">
              Save
            </button>
            <button type="button" className="btn sm" onClick={() => setAdding(false)}>
              Cancel
            </button>
          </div>
        </form>
      )}
      {err && <p className="err">{err}</p>}
      <div className="sec">
        {list === null ? (
          <span className="spinner" />
        ) : list.length === 0 ? (
          <p className="empty-note">Nothing scheduled. Ask Alexa to do something “every morning” or “tomorrow at 9”, or add one here. When one comes due, Alexa picks it up in your conversation.</p>
        ) : (
          <ul className="list">
            {list.map((s) => (
              <li key={s.id}>
                <div className="main">
                  <div className="t1">{s.name}</div>
                  <div className="t2">
                    {s.cron ? describeCron(s.cron) : `Once, ${fmtWhen(s.runAt)}`}
                    {s.enabled && s.nextRun ? ` · next ${fmtWhen(s.nextRun)}` : ''}
                    {!s.enabled ? ' · paused' : ''}
                  </div>
                  <div className="t2">{s.prompt}</div>
                  {s.lastRun ? <div className="t2">Last ran {ago(s.lastRun)}</div> : null}
                </div>
                <button
                  type="button"
                  className="icon-btn muted"
                  style={{ width: 36, height: 36 }}
                  aria-label={`Run ${s.name} now`}
                  onClick={async () => {
                    await api.runSchedule(s.id)
                    load()
                  }}
                >
                  <Play size={16} />
                </button>
                <button
                  type="button"
                  className="toggle"
                  role="switch"
                  aria-checked={s.enabled}
                  aria-label={`${s.name} enabled`}
                  style={{ marginTop: 6 }}
                  onClick={async () => {
                    await api.setScheduleEnabled(s.id, !s.enabled)
                    load()
                  }}
                />
                <button
                  type="button"
                  className="icon-btn muted"
                  style={{ width: 36, height: 36 }}
                  aria-label={`Delete ${s.name}`}
                  onClick={async () => {
                    await api.deleteSchedule(s.id)
                    load()
                  }}
                >
                  <Trash size={16} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Sheet>
  )
}

// ---------- memory ----------

export function MemorySheet({ onClose }: { onClose: () => void }) {
  const [list, setList] = useState<Memory[] | null>(null)
  const [text, setText] = useState('')
  const [editing, setEditing] = useState<{ id: number; content: string } | null>(null)
  const load = () => api.memories().then(setList)
  useEffect(() => {
    load()
  }, [])
  return (
    <Sheet title="Memory" onClose={onClose}>
      <p className="empty-note" style={{ paddingTop: 0 }}>
        Things Alexa remembers about you, for good. It adds to this as it learns; you can edit or remove anything.
      </p>
      <form
        className="inline"
        onSubmit={async (e) => {
          e.preventDefault()
          if (!text.trim()) return
          await api.addMemory(text.trim())
          setText('')
          load()
        }}
      >
        <div className="field" style={{ marginTop: 0 }}>
          <label htmlFor="mem-add" className="sr">
            New memory
          </label>
          <input id="mem-add" value={text} onChange={(e) => setText(e.target.value)} placeholder="e.g. I’m vegetarian" />
        </div>
        <button type="submit" className="btn">
          Add
        </button>
      </form>
      <div className="sec">
        {list === null ? (
          <span className="spinner" />
        ) : list.length === 0 ? (
          <p className="empty-note">Nothing remembered yet.</p>
        ) : (
          <ul className="list">
            {list.map((m) => (
              <li key={m.id}>
                <div className="main">
                  {editing?.id === m.id ? (
                    <form
                      onSubmit={async (e) => {
                        e.preventDefault()
                        await api.updateMemory(m.id, editing.content)
                        setEditing(null)
                        load()
                      }}
                    >
                      <div className="field" style={{ marginTop: 0 }}>
                        <textarea aria-label="Edit memory" value={editing.content} onChange={(e) => setEditing({ id: m.id, content: e.target.value })} autoFocus />
                      </div>
                      <div className="row-btns" style={{ justifyContent: 'flex-start', marginTop: 8 }}>
                        <button type="submit" className="btn xs primary">
                          Save
                        </button>
                        <button type="button" className="btn xs" onClick={() => setEditing(null)}>
                          Cancel
                        </button>
                      </div>
                    </form>
                  ) : (
                    <button type="button" className="rowlink" onClick={() => setEditing({ id: m.id, content: m.content })}>
                      <div className="t1" style={{ fontWeight: 400 }}>
                        {m.content}
                      </div>
                      <div className="t2">{ago(m.createdAt)}</div>
                    </button>
                  )}
                </div>
                <button
                  type="button"
                  className="icon-btn muted"
                  style={{ width: 36, height: 36 }}
                  aria-label="Forget"
                  onClick={async () => {
                    await api.deleteMemory(m.id)
                    load()
                  }}
                >
                  <Trash size={16} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Sheet>
  )
}

// ---------- logins ----------

// LoginsSheet lists the logins Alexa can sign in with. Passwords go in and
// never come back out.
export function LoginsSheet({ onClose }: { onClose: () => void }) {
  const [list, setList] = useState<Login[] | null>(null)
  const [adding, setAdding] = useState(false)
  const [form, setForm] = useState({ site: '', username: '', password: '' })
  const [changing, setChanging] = useState<{ id: number; password: string } | null>(null)
  const [err, setErr] = useState('')
  const load = () => api.logins().then(setList).catch((e) => setErr(e.message))
  useEffect(() => {
    load()
  }, [])
  return (
    <Sheet
      title="Logins"
      onClose={onClose}
      actions={
        !adding && (
          <button type="button" className="btn xs" onClick={() => setAdding(true)}>
            Add
          </button>
        )
      }
    >
      <p className="empty-note" style={{ paddingTop: 0 }}>
        Alexa signs in with these when a site asks, for example after a session expires. Passwords are encrypted on your server, are never shown to Alexa’s model or sent back to this app, and are only typed into the site they belong to.
      </p>
      {adding && (
        <form
          className="sec"
          autoComplete="off"
          onSubmit={async (e) => {
            e.preventDefault()
            setErr('')
            try {
              await api.addLogin({ ...form, site: siteAddress(form.site) })
              setAdding(false)
              setForm({ site: '', username: '', password: '' })
              load()
            } catch (x) {
              setErr((x as Error).message)
            }
          }}
        >
          <div className="field">
            <label htmlFor="lg-site">Site</label>
            <input id="lg-site" required autoCapitalize="none" autoCorrect="off" spellCheck={false} value={form.site} onChange={(e) => setForm({ ...form, site: e.target.value })} placeholder="amazon.com" />
          </div>
          <div className="inline">
            <div className="field">
              <label htmlFor="lg-user">Username or email</label>
              <input id="lg-user" required autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} data-1p-ignore="" data-lpignore="true" />
            </div>
            <div className="field">
              <label htmlFor="lg-pw">Password</label>
              <input id="lg-pw" required type="password" autoComplete="off" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} data-1p-ignore="" data-lpignore="true" />
            </div>
          </div>
          <div className="row-btns" style={{ marginTop: 14 }}>
            <button type="submit" className="btn primary sm">
              Save
            </button>
            <button type="button" className="btn sm" onClick={() => setAdding(false)}>
              Cancel
            </button>
          </div>
        </form>
      )}
      {err && <p className="err">{err}</p>}
      <div className="sec">
        {list === null ? (
          <span className="spinner" />
        ) : list.length === 0 ? (
          <p className="empty-note">No saved logins yet. When Alexa needs to sign in somewhere, it asks you in the app, and you can save the login for next time.</p>
        ) : (
          <ul className="list">
            {list.map((l) => (
              <li key={l.id}>
                <div className="main">
                  <div className="t1">{l.site}</div>
                  <div className="t2">
                    {l.username} · {l.usedAt ? `used ${ago(l.usedAt)}` : `saved ${ago(l.updatedAt)}`}
                  </div>
                  {changing?.id === l.id && (
                    <form
                      className="inline"
                      style={{ marginTop: 8 }}
                      autoComplete="off"
                      onSubmit={async (e) => {
                        e.preventDefault()
                        setErr('')
                        try {
                          await api.updateLogin(l.id, { password: changing.password })
                          setChanging(null)
                          load()
                        } catch (x) {
                          setErr((x as Error).message)
                        }
                      }}
                    >
                      <div className="field" style={{ marginTop: 0 }}>
                        <label htmlFor={`lg-new-${l.id}`} className="sr">
                          New password for {l.site}
                        </label>
                        <input id={`lg-new-${l.id}`} type="password" required autoFocus autoComplete="off" placeholder="New password" value={changing.password} onChange={(e) => setChanging({ id: l.id, password: e.target.value })} data-1p-ignore="" data-lpignore="true" />
                      </div>
                      <button type="submit" className="btn sm primary">
                        Save
                      </button>
                      <button type="button" className="btn sm" onClick={() => setChanging(null)}>
                        Cancel
                      </button>
                    </form>
                  )}
                </div>
                {changing?.id !== l.id && (
                  <button type="button" className="btn xs" onClick={() => setChanging({ id: l.id, password: '' })}>
                    Change password
                  </button>
                )}
                <button
                  type="button"
                  className="icon-btn muted"
                  style={{ width: 36, height: 36 }}
                  aria-label={`Delete the login for ${l.site}`}
                  onClick={async () => {
                    if (!confirm(`Delete the login for ${l.site} (${l.username})?`)) return
                    await api.deleteLogin(l.id).catch((e) => setErr(e.message))
                    load()
                  }}
                >
                  <Trash size={16} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Sheet>
  )
}

// ---------- notifications ----------

export function NotificationsSheet({ items, onOpenRef, onClose }: { items: Notification[]; onOpenRef: (ref?: string) => void; onClose: () => void }) {
  useEffect(() => {
    if (items.some((n) => !n.read)) api.markRead().catch(() => {})
  }, [items])
  return (
    <Sheet title="Notifications" onClose={onClose}>
      {items.length === 0 ? (
        <p className="empty-note">You’re all caught up.</p>
      ) : (
        <ul className="list">
          {items.map((n) => (
            <li key={n.id}>
              <button type="button" className="rowlink main" onClick={() => onOpenRef(n.ref)}>
                <div className="t1">
                  {!n.read && <span className="dot" style={{ display: 'inline-block', marginRight: 8, animation: 'none' }} />}
                  {n.title}
                </div>
                <div className="t2">{n.body}</div>
                <div className="t2">{ago(n.createdAt)}</div>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Sheet>
  )
}

// ---------- settings ----------

function urlB64ToUint8Array(b64: string) {
  const pad = '='.repeat((4 - (b64.length % 4)) % 4)
  const raw = atob((b64 + pad).replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(raw, (c) => c.charCodeAt(0))
}

export async function enablePush(config: ServerConfig) {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) throw new Error('This browser doesn’t support push notifications (on iPhone, add Alexa to your home screen first).')
  const perm = await Notification.requestPermission()
  if (perm !== 'granted') throw new Error('Notification permission was not granted.')
  const reg = await navigator.serviceWorker.ready
  let sub = await reg.pushManager.getSubscription()
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: urlB64ToUint8Array(config.vapidPublicKey) })
  await api.pushSubscribe(sub.toJSON())
}

const zones = (() => {
  try {
    return (Intl as unknown as { supportedValuesOf: (k: string) => string[] }).supportedValuesOf('timeZone')
  } catch {
    return []
  }
})()

type ConversationProps = { session: SessionState; onCompact: () => Promise<void>; onReset: () => Promise<void> }

export function SettingsSheet({ user, config, onSaved, onClose, conversation }: { user: User; config: ServerConfig | null; onSaved: (u: User) => void; onClose: () => void; conversation: ConversationProps }) {
  const [f, setF] = useState({ displayName: user.displayName, email: user.email, timezone: user.timezone, about: user.about })
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [pw, setPw] = useState({ current: '', next: '' })
  const [pwMsg, setPwMsg] = useState('')
  const [pushMsg, setPushMsg] = useState('')
  const pushOn = typeof Notification !== 'undefined' && Notification.permission === 'granted'
  return (
    <Sheet title="Settings" onClose={onClose}>
      <form
        className="sec"
        onSubmit={async (e) => {
          e.preventDefault()
          setMsg('')
          setErr('')
          try {
            onSaved(await api.updateMe(f))
            setMsg('Saved')
          } catch (x) {
            setErr((x as Error).message)
          }
        }}
      >
        <p className="sec-title">Profile</p>
        <div className="field">
          <label htmlFor="s-name">Name</label>
          <input id="s-name" value={f.displayName} onChange={(e) => setF({ ...f, displayName: e.target.value })} />
        </div>
        <div className="field">
          <label htmlFor="s-tz">Timezone</label>
          <select id="s-tz" value={f.timezone} onChange={(e) => setF({ ...f, timezone: e.target.value })}>
            {(zones.includes(f.timezone) ? zones : [f.timezone, ...zones]).map((z) => (
              <option key={z}>{z}</option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="s-email">Email for notifications</label>
          <input id="s-email" type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} placeholder="you@example.com" />
          <span className="help">{config?.emailEnabled ? 'Alexa can email you when you ask it to.' : 'Email delivery isn’t configured on this server (set RESEND_API_KEY).'}</span>
        </div>
        <div className="field">
          <label htmlFor="s-about">About you</label>
          <textarea id="s-about" value={f.about} onChange={(e) => setF({ ...f, about: e.target.value })} placeholder="Where you live, what you do, preferences Alexa should always keep in mind…" />
        </div>
        <button type="submit" className="btn primary sm" style={{ marginTop: 14 }}>
          Save
        </button>
        {msg && <p className="okmsg">{msg}</p>}
        {err && <p className="err">{err}</p>}
      </form>

      <Appearance />

      <div className="sec">
        <p className="sec-title">Notifications</p>
        <p className="empty-note" style={{ paddingTop: 0 }}>
          When Alexa says something while you’re away, it arrives as a push notification on this device.
        </p>
        <button
          type="button"
          className="btn sm"
          onClick={async () => {
            setPushMsg('')
            try {
              if (!config) throw new Error('Not ready yet')
              await enablePush(config)
              setPushMsg('Push notifications are on for this device.')
            } catch (x) {
              setPushMsg((x as Error).message)
            }
          }}
        >
          {pushOn ? 'Re-register this device' : 'Enable on this device'}
        </button>
        {pushMsg && <p className="okmsg">{pushMsg}</p>}
      </div>

      <form
        className="sec"
        onSubmit={async (e) => {
          e.preventDefault()
          setPwMsg('')
          try {
            await api.changePassword(pw.current, pw.next)
            setPw({ current: '', next: '' })
            setPwMsg('Password changed')
          } catch (x) {
            setPwMsg((x as Error).message)
          }
        }}
      >
        <p className="sec-title">Password</p>
        <div className="inline">
          <div className="field">
            <label htmlFor="pw-cur">Current</label>
            <input id="pw-cur" type="password" autoComplete="current-password" value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} />
          </div>
          <div className="field">
            <label htmlFor="pw-new">New</label>
            <input id="pw-new" type="password" autoComplete="new-password" minLength={8} value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} />
          </div>
        </div>
        <button type="submit" className="btn sm" style={{ marginTop: 12 }} disabled={!pw.current || pw.next.length < 8}>
          Change password
        </button>
        {pwMsg && <p className="okmsg">{pwMsg}</p>}
      </form>

      <ConversationSection {...conversation} />

      {config && (
        <div className="sec">
          <p className="sec-title">Server</p>
          <p className="empty-note" style={{ paddingTop: 0 }}>
            Model {config.model} · browser {config.viewport.width}×{config.viewport.height}
            {config.sandboxed ? ' · per-user sandbox' : ''}
            {config.shellEnabled ? ' · shell tool on' : ''}
          </p>
        </div>
      )}
    </Sheet>
  )
}

const themes: [Theme, string][] = [
  ['system', 'System'],
  ['light', 'Light'],
  ['dark', 'Dark'],
]

function Appearance() {
  const [theme, set] = useState(getTheme)
  return (
    <div className="sec">
      <p className="sec-title">Appearance</p>
      <div className="seg" role="radiogroup" aria-label="Appearance">
        {themes.map(([t, label]) => (
          <button
            key={t}
            type="button"
            role="radio"
            aria-checked={theme === t}
            onClick={() => {
              setTheme(t)
              set(t)
            }}
          >
            {label}
          </button>
        ))}
      </div>
      <p className="field help" style={{ marginTop: 8 }}>
        On this device. System follows your device’s light or dark setting.
      </p>
    </div>
  )
}

function ConversationSection({ session, onCompact, onReset }: ConversationProps) {
  const [busy, setBusy] = useState('')
  const [msg, setMsg] = useState('')
  const idle = session.status !== 'working'
  const run = (what: string, fn: () => Promise<void>, done: string) => async () => {
    setBusy(what)
    setMsg('')
    try {
      await fn()
      setMsg(done)
    } catch (x) {
      setMsg((x as Error).message)
    } finally {
      setBusy('')
    }
  }
  return (
    <div className="sec">
      <p className="sec-title">Conversation</p>
      <p className="empty-note" style={{ paddingTop: 0 }}>
        Alexa keeps one ongoing conversation with you. When it gets long, the older part is summarised so Alexa can carry on
        {session.compactions > 0 ? ` (${session.compactions} ${session.compactions === 1 ? 'time' : 'times'} so far)` : ''}. Lasting facts go to Memory.
        {session.contextTokens > 0 ? ` Current context: ${Math.round(session.contextTokens / 1000)}k tokens.` : ''}
      </p>
      <div className="row-btns" style={{ justifyContent: 'flex-start' }}>
        <button type="button" className="btn sm" disabled={!idle || !!busy} onClick={run('compact', onCompact, 'Summarised.')}>
          {busy === 'compact' ? 'Summarising…' : 'Summarise now'}
        </button>
        <button
          type="button"
          className="btn sm danger"
          disabled={!idle || !!busy}
          onClick={async () => {
            if (!confirm('Clear the conversation? Alexa forgets what you talked about (memories, reports, files and schedules stay).')) return
            await run('reset', onReset, 'Cleared.')()
          }}
        >
          Clear conversation
        </button>
      </div>
      {!idle && <p className="field help" style={{ marginTop: 8 }}>Available when Alexa isn’t working.</p>}
      {msg && <p className="okmsg">{msg}</p>}
    </div>
  )
}

// ---------- users (admin) ----------

export function UsersSheet({ me, onClose }: { me: User; onClose: () => void }) {
  const [list, setList] = useState<User[] | null>(null)
  const [f, setF] = useState({ username: '', password: '', displayName: '', isAdmin: false })
  const [err, setErr] = useState('')
  const load = () => api.users().then(setList)
  useEffect(() => {
    load()
  }, [])
  return (
    <Sheet title="Users" onClose={onClose}>
      <p className="empty-note" style={{ paddingTop: 0 }}>
        Each person gets their own conversation with Alexa, memory, files and browser profile.
      </p>
      <form
        className="sec"
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          try {
            await api.createUser(f)
            setF({ username: '', password: '', displayName: '', isAdmin: false })
            load()
          } catch (x) {
            setErr((x as Error).message)
          }
        }}
      >
        <p className="sec-title">Add someone</p>
        <div className="inline">
          <div className="field">
            <label htmlFor="u-name">Username</label>
            <input id="u-name" required value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} autoComplete="off" />
          </div>
          <div className="field">
            <label htmlFor="u-pw">Password</label>
            <input id="u-pw" required type="password" minLength={8} value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} autoComplete="new-password" />
          </div>
        </div>
        <div className="field">
          <label htmlFor="u-dn">Display name</label>
          <input id="u-dn" value={f.displayName} onChange={(e) => setF({ ...f, displayName: e.target.value })} />
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 14 }}>
          <button type="button" className="toggle" role="switch" aria-checked={f.isAdmin} aria-label="Admin" onClick={() => setF({ ...f, isAdmin: !f.isAdmin })} />
          <span style={{ fontSize: 14 }}>Admin</span>
        </div>
        <button type="submit" className="btn primary sm" style={{ marginTop: 14 }}>
          Add user
        </button>
        {err && <p className="err">{err}</p>}
      </form>
      <div className="sec">
        <ul className="list">
          {(list || []).map((u) => (
            <li key={u.id}>
              <div className="main">
                <div className="t1">
                  {u.displayName || u.username} {u.isAdmin && <span className="pill soft">admin</span>}
                </div>
                <div className="t2">
                  @{u.username} · joined {ago(u.createdAt)}
                </div>
              </div>
              {u.id !== me.id && (
                <>
                  <button
                    type="button"
                    className="btn xs"
                    onClick={async () => {
                      const p = prompt(`New password for ${u.username} (min 8 chars)`)
                      if (p) await api.resetPassword(u.id, p).catch((x) => setErr(x.message))
                    }}
                  >
                    Reset password
                  </button>
                  <button
                    type="button"
                    className="icon-btn muted"
                    style={{ width: 36, height: 36 }}
                    aria-label={`Delete ${u.username}`}
                    onClick={async () => {
                      if (!confirm(`Delete ${u.username} and all their data?`)) return
                      await api.deleteUser(u.id)
                      load()
                    }}
                  >
                    <Trash size={16} />
                  </button>
                </>
              )}
            </li>
          ))}
        </ul>
      </div>
    </Sheet>
  )
}
