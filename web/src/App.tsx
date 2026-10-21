import { useCallback, useEffect, useRef, useState } from 'react'
import { Auth } from './components/Auth'
import { Composer } from './components/Composer'
import { History } from './components/Icons'
import { Log } from './components/Log'
import { BrowserPanel, Caption, ReportPanel, type Live } from './components/Panels'
import { Reflect, type ReflectTab } from './components/Reflect'
import { AccountMenu, LoginsSheet, MemorySheet, NotificationsSheet, SchedulesSheet, SettingsSheet, UsersSheet, type SheetName } from './components/Sheets'
import type { Attachment, Entry, Notification, Report, ServerConfig, SessionState, Surface, User } from './lib/api'
import { api, ApiError } from './lib/api'
import { useEvents, type ServerEvent } from './lib/events'
import { useExchange } from './lib/exchange'
import { useExit } from './lib/useExit'
import { speak } from './lib/voice'

type Toast = { id: number; title: string; body: string }

const blank: SessionState = { status: 'idle', activity: '', surface: { kind: '' }, control: false, contextTokens: 0, compactions: 0 }

function greeting(name: string) {
  const h = new Date().getHours()
  const part = h < 5 ? 'Up late' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
  return `${part}${name ? `, ${name.split(' ')[0]}` : ''}`
}

// upsert keeps the feed in id order: events from different goroutines can
// arrive slightly out of order.
function upsert(feed: Entry[], e: Entry) {
  const i = feed.findIndex((x) => x.id === e.id)
  if (i >= 0) {
    const n = feed.slice()
    n[i] = e
    return n
  }
  if (!feed.length || feed[feed.length - 1].id < e.id) return [...feed, e]
  const at = feed.findIndex((x) => x.id > e.id)
  return [...feed.slice(0, at), e, ...feed.slice(at)]
}

export default function App() {
  const [boot, setBoot] = useState<'loading' | 'auth' | 'ready'>('loading')
  const [setup, setSetup] = useState({ needsSetup: false, signupAllowed: false })
  const [user, setUser] = useState<User | null>(null)
  const [config, setConfig] = useState<ServerConfig | null>(null)
  const [state, setState] = useState<SessionState>(blank)
  const [feed, setFeed] = useState<Entry[]>([])
  const [loaded, setLoaded] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [reports, setReports] = useState<Record<string, Report>>({})
  const [live, setLive] = useState<Live>({ url: '', cursor: null })
  const [voice, setVoice] = useState(false)
  const [heard, setHeard] = useState({ committed: '', partial: '' })
  const [menu, setMenu] = useState(false)
  const [sheet, setSheet] = useState<SheetName | null>(null)
  const [reflect, setReflect] = useState(false)
  const [reflectTab, setReflectTab] = useState<ReflectTab>('conversation')
  const [notes, setNotes] = useState<Notification[]>([])
  const [toasts, setToasts] = useState<Toast[]>([])
  // After the user speaks, Alexa's replies are read aloud.
  const speaking = useRef(false)
  const spokenUpTo = useRef(0)
  const feedRef = useRef(feed)
  feedRef.current = feed

  const toast = useCallback((title: string, body = '') => {
    const id = Date.now() + Math.random()
    setToasts((ts) => [...ts.slice(-2), { id, title, body }])
    setTimeout(() => setToasts((ts) => ts.filter((t) => t.id !== id)), 6000)
  }, [])

  // ---------- boot ----------
  useEffect(() => {
    ;(async () => {
      try {
        const s = await api.setup()
        setSetup(s)
        if (s.needsSetup) return setBoot('auth')
        const u = await api.me()
        setUser(u)
        setBoot('ready')
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) setBoot('auth')
        else {
          setBoot('auth')
          toast('Can’t reach the server', (e as Error).message)
        }
      }
    })()
  }, [toast])

  const load = useCallback(async () => {
    const [s, ns] = await Promise.all([api.session(), api.notifications()])
    setState(s.state)
    setFeed(s.feed)
    setHasMore(s.hasMore)
    if (s.report) setReports((m) => ({ ...m, [s.report!.id]: s.report! }))
    setNotes(ns)
    setLoaded(true)
  }, [])

  // Reflect opens as a page of its own, so the browser's back button
  // closes it.
  const openReflect = (tab?: ReflectTab) => {
    if (tab) setReflectTab(tab)
    setMenu(false)
    setSheet(null)
    if (!history.state?.reflect) history.pushState({ reflect: true }, '')
    setReflect(true)
  }
  const closeReflect = useCallback(() => {
    if (history.state?.reflect) history.back()
    else setReflect(false)
  }, [])
  useEffect(() => {
    const onPop = () => setReflect(!!history.state?.reflect)
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  // The screen follows the session; these let the user steer it too.
  const show = useCallback(
    (s: Surface) => {
      setSheet(null)
      setMenu(false)
      closeReflect()
      setState((st) => ({ ...st, surface: s }))
      api.setSurface(s).catch((e) => toast('Couldn’t open that', (e as Error).message))
    },
    [toast, closeReflect],
  )
  const openReport = useCallback((id: string) => show({ kind: 'report', id }), [show])
  const openRef = useCallback(
    (ref?: string) => {
      if (ref?.startsWith('report:')) openReport(ref.slice(7))
      else {
        setSheet(null)
        setMenu(false)
        closeReflect()
      }
    },
    [openReport, closeReflect],
  )

  useEffect(() => {
    if (boot !== 'ready') return
    load().catch((e) => toast('Couldn’t load the conversation', (e as Error).message))
    api.config().then(setConfig).catch(() => {})
    const q = new URLSearchParams(location.search).get('report')
    if (q) openReport(q)
    if (location.search || history.state?.reflect) history.replaceState(null, '', '/')
  }, [boot, load, openReport, toast])

  // Notification clicks (from the service worker) open what they're about.
  useEffect(() => {
    if (!('serviceWorker' in navigator)) return
    const h = (e: MessageEvent) => {
      if (e.data?.type === 'open') openRef(e.data.ref)
    }
    navigator.serviceWorker.addEventListener('message', h)
    return () => navigator.serviceWorker.removeEventListener('message', h)
  }, [openRef])

  // Fetch the report on screen if this client hasn't seen it yet.
  const reportId = state.surface.kind === 'report' ? state.surface.id : undefined
  const haveReport = reportId ? !!reports[reportId] : true
  useEffect(() => {
    if (!reportId || haveReport) return
    api
      .report(reportId)
      .then((r) => setReports((m) => ({ ...m, [r.id]: r })))
      .catch(() => show({ kind: '' }))
  }, [reportId, haveReport, show])

  useEffect(() => {
    document.title = state.status === 'needs' ? '(1) Alexa' : 'Alexa'
  }, [state.status])

  // ---------- live events ----------
  const onEvent = useCallback(
    (e: ServerEvent) => {
      switch (e.type) {
        case 'state':
          setState(e.data as SessionState)
          break
        case 'entry': {
          const en = e.data as Entry
          setFeed((f) => upsert(f, en))
          if (speaking.current && (en.kind === 'say' || en.kind === 'ask') && en.id > spokenUpTo.current) {
            spokenUpTo.current = en.id
            speak(en.text, () => {})
          }
          break
        }
        case 'entryStatus': {
          const d = e.data as { id: number; status: string }
          setFeed((f) => f.map((x) => (x.id === d.id ? { ...x, status: d.status } : x)))
          break
        }
        case 'report': {
          const r = e.data as Report
          setReports((m) => ({ ...m, [r.id]: r }))
          break
        }
        case 'browser': {
          const d = e.data as { type: string; url?: string; x?: number; y?: number; click?: boolean }
          if (d.type === 'nav') setLive((l) => ({ ...l, url: d.url || l.url }))
          if (d.type === 'cursor') setLive((l) => ({ ...l, cursor: { x: d.x || 0, y: d.y || 0, click: !!d.click, seq: (l.cursor?.seq || 0) + 1 } }))
          break
        }
        case 'notification': {
          const n = e.data as Notification
          setNotes((ns) => [n, ...ns.filter((x) => x.id !== n.id)])
          break
        }
        case 'reset':
          setFeed([])
          setHasMore(false)
          break
      }
    },
    [],
  )
  useEvents(boot === 'ready', onEvent, () => {
    load().catch(() => {})
  })

  const { entries: exchange, clear } = useExchange(feed, state.status === 'idle', user?.id ?? 0, loaded)

  // What the screen shows next to the exchange follows the session; it
  // stays a moment after it closes, to animate out.
  const showBrowser = state.surface.kind === 'browser' || state.control
  const { shown: panelKey, leaving: panelLeaving } = useExit(showBrowser ? 'browser' : reportId ? `report:${reportId}` : null, 380)
  const { shown: reflectShown, leaving: reflectLeaving } = useExit(reflect ? true : null, 300)

  // ---------- actions ----------
  const send = async (text: string, attachments: Attachment[], viaVoice: boolean) => {
    speaking.current = viaVoice
    const f = feedRef.current
    spokenUpTo.current = f.length ? f[f.length - 1].id : 0
    try {
      await api.send(text, attachments, viaVoice)
    } catch (e) {
      toast('Couldn’t send', (e as Error).message)
    }
  }

  const act = (fn: () => Promise<unknown>) => () => {
    fn().catch((e) => toast('Something went wrong', (e as Error).message))
  }

  const loadMore = async () => {
    const f = feedRef.current
    if (!f.length) return
    try {
      const r = await api.feed(f[0].id)
      setFeed((cur) => [...r.feed.filter((x) => x.id < (cur[0]?.id ?? Infinity)), ...cur])
      setHasMore(r.hasMore)
    } catch (e) {
      toast('Couldn’t load earlier messages', (e as Error).message)
    }
  }

  // ---------- render ----------
  if (boot === 'loading')
    return (
      <div className="app">
        <div className="auth">
          <span className="spinner" />
        </div>
      </div>
    )
  if (boot === 'auth' || !user)
    return (
      <div className="app">
        <Auth
          needsSetup={setup.needsSetup}
          signupAllowed={setup.signupAllowed}
          onDone={(u) => {
            setUser(u)
            setBoot('ready')
          }}
        />
        <Toasts toasts={toasts} />
      </div>
    )

  const unread = notes.filter((n) => !n.read).length
  const working = state.status === 'working'
  const needs = state.status === 'needs' ? state.needs : null
  const placeholder =
    needs?.kind === 'question' ? 'Type your answer' : needs?.kind === 'takeover' ? 'Tell Alexa something, or take over' : needs?.kind === 'login' ? 'Or tell Alexa something instead' : working ? 'Add something or change course' : 'Ask Alexa to do anything'

  const takeOver = act(() => api.takeOver())
  const handBack = act(() => api.handBack())
  const stop = act(() => api.stop())
  const answer = (t: string) => send(t, [], false)

  // The mark takes you home: the panel closes and, once Alexa is done, the
  // exchange is put away (it's in Reflect).
  const home = () => {
    if (state.surface.kind) show({ kind: '' })
    if (state.status === 'idle') clear()
  }

  let panel: React.ReactNode = null
  if (panelKey === 'browser')
    panel = <BrowserPanel state={state} live={live} onTakeOver={takeOver} onHandBack={handBack} onClose={() => show({ kind: '' })} onUrl={(u) => setLive((l) => ({ ...l, url: u }))} />
  else if (panelKey?.startsWith('report:')) {
    const id = panelKey.slice(7)
    panel = <ReportPanel key={id} report={reports[id] || null} onClose={() => show({ kind: '' })} />
  }

  // The last thing Alexa said since the user last spoke, for small screens.
  let latest: Entry | null = null
  for (let i = exchange.length - 1; i >= 0; i--) {
    const k = exchange[i].kind
    if (k === 'user') break
    if (k === 'say' || k === 'error') {
      latest = exchange[i]
      break
    }
  }

  return (
    <div className={`app${voice ? ' voice' : ''}`}>
      <header className="header">
        <button type="button" className="mark-btn" aria-label="Home" title="Home" onClick={home}>
          <span className={`mark${working ? ' busy' : ''}`} />
        </button>
        <div className="end">
          <button type="button" className="icon-btn" aria-label="Reflect" title="Reflect" onClick={() => openReflect()}>
            <History size={20} />
          </button>
          <button type="button" className="avatar-btn" aria-label="Account menu" aria-expanded={menu} onClick={() => setMenu(!menu)}>
            <span className="avatar">{(user.displayName || user.username).slice(0, 1).toUpperCase()}</span>
            {(unread > 0 || needs) && <span className="badge" />}
          </button>
        </div>
      </header>

      <main className={`stage${panel ? ' with-panel' : ''}${panelLeaving ? ' closing' : ''}`}>
        {panel && (
          <div className="panel-col">
            {panel}
            <Caption state={state} latest={latest} onAnswer={answer} onTakeOver={takeOver} onHandBack={handBack} onStop={stop} />
          </div>
        )}
        <Log
          mode="now"
          entries={exchange}
          state={state}
          greeting={greeting(user.displayName)}
          onAnswer={answer}
          onTakeOver={takeOver}
          onHandBack={handBack}
          onStop={stop}
          onOpenReport={openReport}
          onShowBrowser={working && !showBrowser && state.browserUrl ? () => show({ kind: 'browser' }) : undefined}
        />
      </main>

      {voice && (
        <div className="voice-tray" aria-live="polite">
          <p className={`heard${heard.committed || heard.partial ? '' : ' quiet'}`}>
            <span>
              {heard.committed || heard.partial ? (
                <>
                  {heard.committed} <span className="saying">{heard.partial}</span>
                </>
              ) : (
                'Listening…'
              )}
            </span>
          </p>
          <p className="hint">{working ? 'Add something or change course. Tap the circle to send.' : 'Say what you need. Tap the circle when you’re done.'}</p>
        </div>
      )}

      <Composer placeholder={placeholder} voice={voice} onVoice={setVoice} onSend={send} onListen={(c, p) => setHeard({ committed: c, partial: p })} onError={(m) => toast('Voice', m)} />

      {reflectShown && (
        <Reflect tab={reflectTab} onTab={setReflectTab} leaving={reflectLeaving} feed={feed} hasMore={hasMore} state={state} onMore={loadMore} onOpenReport={openReport} onClose={closeReflect} />
      )}

      {menu && (
        <AccountMenu
          user={user}
          unread={unread}
          onClose={() => setMenu(false)}
          onOpen={(s) => {
            setMenu(false)
            setSheet(s)
            if (s === 'notifications') api.notifications().then(setNotes)
          }}
          onReflect={() => openReflect()}
          onBrowser={() => show({ kind: 'browser' })}
          onSignOut={async () => {
            await api.logout().catch(() => {})
            location.href = '/'
          }}
        />
      )}
      {sheet === 'schedules' && <SchedulesSheet onClose={() => setSheet(null)} />}
      {sheet === 'memory' && <MemorySheet onClose={() => setSheet(null)} />}
      {sheet === 'logins' && <LoginsSheet onClose={() => setSheet(null)} />}
      {sheet === 'notifications' && (
        <NotificationsSheet
          items={notes}
          onOpenRef={openRef}
          onClose={() => {
            setSheet(null)
            setNotes((ns) => ns.map((n) => ({ ...n, read: true })))
          }}
        />
      )}
      {sheet === 'settings' && (
        <SettingsSheet
          user={user}
          config={config}
          onSaved={setUser}
          onClose={() => setSheet(null)}
          conversation={{
            session: state,
            onCompact: async () => {
              setState(await api.compact())
            },
            onReset: async () => {
              await api.reset()
              await load()
            },
          }}
        />
      )}
      {sheet === 'users' && user.isAdmin && <UsersSheet me={user} onClose={() => setSheet(null)} />}

      <Toasts toasts={toasts} />
    </div>
  )
}

function Toasts({ toasts }: { toasts: Toast[] }) {
  if (!toasts.length) return null
  return (
    <div className="toasts" role="status">
      {toasts.map((t) => (
        <div key={t.id} className="toast">
          <div>
            <b>{t.title}</b>
            {t.body && <span className="b">{t.body}</span>}
          </div>
        </div>
      ))}
    </div>
  )
}
