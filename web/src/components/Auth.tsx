import { useState } from 'react'
import type { User } from '../lib/api'
import { api } from '../lib/api'

const tz = Intl.DateTimeFormat().resolvedOptions().timeZone

export function Auth({ needsSetup, signupAllowed, onDone }: { needsSetup: boolean; signupAllowed: boolean; onDone: (u: User) => void }) {
  const [mode, setMode] = useState<'login' | 'signup'>(needsSetup ? 'signup' : 'login')
  const [f, setF] = useState({ username: '', password: '', displayName: '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const signup = mode === 'signup'
  return (
    <div className="auth">
      <form
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          setBusy(true)
          try {
            const u = signup ? await api.signup({ ...f, timezone: tz }) : await api.login({ username: f.username, password: f.password, timezone: tz })
            onDone(u)
          } catch (x) {
            setErr((x as Error).message)
          } finally {
            setBusy(false)
          }
        }}
      >
        <span className="mark" style={{ display: 'block', cursor: 'default' }} aria-hidden="true" />
        <h1>{needsSetup ? 'Set up Alexa' : signup ? 'Create account' : 'Welcome back'}</h1>
        <p className="sub">{needsSetup ? 'Create the first account. It will be the admin and can invite others.' : 'Your always-on agent for the web.'}</p>
        {signup && (
          <div className="field">
            <label htmlFor="a-dn">Your name</label>
            <input id="a-dn" value={f.displayName} onChange={(e) => setF({ ...f, displayName: e.target.value })} autoComplete="name" />
          </div>
        )}
        <div className="field">
          <label htmlFor="a-user">Username</label>
          <input id="a-user" required value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} autoComplete="username" autoCapitalize="none" autoFocus />
        </div>
        <div className="field">
          <label htmlFor="a-pw">Password</label>
          <input id="a-pw" required type="password" minLength={signup ? 8 : undefined} value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} autoComplete={signup ? 'new-password' : 'current-password'} />
        </div>
        {err && <p className="err">{err}</p>}
        <button type="submit" className="btn primary" disabled={busy}>
          {busy ? <span className="spinner" /> : signup ? 'Create account' : 'Sign in'}
        </button>
        {!needsSetup && signupAllowed && (
          <button type="button" className="btn" style={{ marginTop: 8, border: 0 }} onClick={() => setMode(signup ? 'login' : 'signup')}>
            {signup ? 'I have an account' : 'Create an account'}
          </button>
        )}
      </form>
    </div>
  )
}
