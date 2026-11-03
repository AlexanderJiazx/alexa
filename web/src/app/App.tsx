import { useEffect } from 'react'
import { api, ApiError, onUnauthorized } from '../api/client'
import { Auth } from '../auth/Auth'
import { speakReplies } from '../composer/spoken'
import { announcer } from '../features/conversation/announce'
import { Palette } from '../features/palette/Palette'
import { followChanges, openRef, openReport, signedOut, syncReflect, toast } from '../state/actions'
import { connectApp, onServerEvent } from '../state/connection'
import { useBoot, useSetup, useStatus, useUI, useUser } from '../state/selectors'
import { setApp } from '../state/store'
import { LiveAnnouncer } from '../ui/LiveAnnouncer'
import { Header } from './Header'
import { OverlayHost, Toasts } from './OverlayHost'
import { Stage } from './Stage'

// App boots the app: it finds out whether someone is signed in, shows
// sign-in if not, and otherwise the shell. Everything else plugs in through
// the Stage, the panel host, the overlay host and the registries.
export default function App() {
  const boot = useBoot()
  const setup = useSetup()
  const user = useUser()

  useEffect(() => {
    ;(async () => {
      try {
        const s = await api.setup()
        setApp({ setup: s })
        if (s.needsSetup) return setApp({ boot: 'auth' })
        const u = await api.me()
        setApp({ user: u, boot: 'ready' })
      } catch (e) {
        setApp({ boot: 'auth' })
        if (!(e instanceof ApiError && e.status === 401)) toast('Can’t reach the server', (e as Error).message)
      }
    })()
  }, [])

  // A request turned away as signed out means the session is gone.
  useEffect(() => {
    onUnauthorized(signedOut)
    return () => onUnauthorized(null)
  }, [])

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
        <Auth needsSetup={setup.needsSetup} signupAllowed={setup.signupAllowed} onDone={(u) => setApp({ user: u, boot: 'ready' })} />
        <Toasts />
      </div>
    )
  return <Shell />
}

// Shell is the signed-in app: connected to the session for as long as it's
// on screen.
function Shell() {
  const voice = useUI((u) => u.voice)
  const status = useStatus()

  useEffect(
    () =>
      connectApp({
        unauthorized: signedOut,
        syncError: (e, first) => {
          if (first) toast('Couldn’t load the conversation', (e as Error).message)
        },
      }),
    [],
  )

  useEffect(() => {
    api
      .config()
      .then((config) => setApp({ config }))
      .catch(() => {})
    const q = new URLSearchParams(location.search).get('report')
    if (q) openReport(q)
    if (location.search || history.state?.reflect) history.replaceState(null, '', '/')
  }, [])

  // After the user speaks, Alexa's replies are read aloud.
  useEffect(() => onServerEvent(speakReplies), [])

  // Screen readers hear each new entry as it arrives.
  useEffect(() => onServerEvent(announcer()), [])

  // A profile or notifications changed on another device.
  useEffect(() => onServerEvent(followChanges), [])

  useEffect(() => {
    window.addEventListener('popstate', syncReflect)
    return () => window.removeEventListener('popstate', syncReflect)
  }, [])

  // Notification clicks (from the service worker) open what they're about.
  useEffect(() => {
    if (!('serviceWorker' in navigator)) return
    const h = (e: MessageEvent) => {
      if (e.data?.type === 'open') openRef(e.data.ref)
    }
    navigator.serviceWorker.addEventListener('message', h)
    return () => navigator.serviceWorker.removeEventListener('message', h)
  }, [])

  useEffect(() => {
    document.title = status === 'needs' ? '(1) Alexa' : 'Alexa'
  }, [status])

  return (
    <div className={`app${voice ? ' voice' : ''}`}>
      <Header />
      <Stage />
      <OverlayHost />
      <Palette />
      <LiveAnnouncer />
    </div>
  )
}
