import { useState } from 'react'
import { api } from '../api/client'
import type { User } from '../api/types'
import { Button } from '../ui/Button'
import { Field, Input } from '../ui/Field'
import { Mark } from '../ui/Mark'
import './auth.css'

const tz = Intl.DateTimeFormat().resolvedOptions().timeZone

type Mode = 'login' | 'signup'

const copy = (setup: boolean, mode: Mode) =>
  setup
    ? { title: 'Set up Alexa', sub: 'Create the first account. It’s the admin, and can invite others.', submit: 'Create account' }
    : mode === 'signup'
      ? { title: 'Create your account', sub: 'Your always-on agent for the web.', submit: 'Create account' }
      : { title: 'Welcome back', sub: 'Your always-on agent for the web.', submit: 'Sign in' }

// said turns what the server says into a sentence for this page: the two
// it says most get their own words, the rest a capital and a full stop.
const said = (msg: string) => {
  if (/wrong username or password/i.test(msg)) return 'That username and password don’t match.'
  if (/too many attempts/i.test(msg)) return 'Too many tries. Try again in a few minutes.'
  const m = msg.trim()
  return m && m[0].toUpperCase() + m.slice(1) + (/[.!?]$/.test(m) ? '' : '.')
}

// Auth is signing in, creating an account, and setting up the first one.
// No card: the dot draws itself in, then the headline, the line under it
// and the form rise onto the page in turn.
export function Auth({ needsSetup, signupAllowed, onDone }: { needsSetup: boolean; signupAllowed: boolean; onDone: (u: User) => void }) {
  const [mode, setMode] = useState<Mode>(needsSetup ? 'signup' : 'login')
  const [f, setF] = useState({ username: '', password: '', displayName: '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const signup = mode === 'signup'
  const c = copy(needsSetup, mode)
  return (
    <main className="auth">
      <form
        className="auth-form"
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          setBusy(true)
          try {
            const u = signup ? await api.signup({ ...f, timezone: tz }) : await api.login({ username: f.username, password: f.password, timezone: tz })
            onDone(u)
          } catch (x) {
            setErr(said((x as Error).message))
          } finally {
            setBusy(false)
          }
        }}
      >
        <Mark size="lg" draw />
        <h1>{c.title}</h1>
        <p className="sub">{c.sub}</p>
        <div className="auth-fields">
          {signup && (
            <Field label="Your name" className="lg">
              {(id) => <Input id={id} value={f.displayName} onChange={(e) => setF({ ...f, displayName: e.target.value })} autoComplete="name" />}
            </Field>
          )}
          <Field label="Username" className="lg">
            {(id) => <Input id={id} required value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} autoComplete="username" autoCapitalize="none" autoCorrect="off" spellCheck={false} autoFocus />}
          </Field>
          <Field label="Password" className="lg" help={signup ? 'At least 8 characters.' : undefined}>
            {(id) => <Input id={id} required type="password" minLength={signup ? 8 : undefined} value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} autoComplete={signup ? 'new-password' : 'current-password'} />}
          </Field>
          {err && (
            <p className="err" role="alert">
              {err}
            </p>
          )}
          <Button type="submit" variant="primary" size="lg" block loading={busy}>
            {c.submit}
          </Button>
          {!needsSetup && signupAllowed && (
            <Button
              variant="ghost"
              size="lg"
              block
              onClick={() => {
                setErr('')
                setMode(signup ? 'login' : 'signup')
              }}
            >
              {signup ? 'I have an account' : 'Create an account'}
            </Button>
          )}
        </div>
      </form>
      <p className="auth-mark">Alexa</p>
    </main>
  )
}
