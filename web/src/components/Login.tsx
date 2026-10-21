import { useId, useState } from 'react'
import { api } from '../lib/api'
import { HandBack, Lock, Pointer, Shield } from './Icons'

type Props = {
  site: string
  username?: string
  reason: string
  // compact is the version in the caption under the browser on small
  // screens.
  compact?: boolean
  control: boolean
  onTakeOver: () => void
  onHandBack: () => void
  onStop: () => void
}

// Password managers would offer this app's own login here, which is never
// what's wanted.
const noManagers = { 'data-1p-ignore': '', 'data-lpignore': 'true', 'data-bwignore': '', 'data-form-type': 'other' }

// LoginForm is how Alexa asks to sign in to a site. What the user types
// goes straight to the server, which saves it encrypted (if they like) and
// types it into that site only. It never enters the conversation, and
// Alexa's model never sees the password.
export function LoginForm({ site, username, reason, compact, control, onTakeOver, onHandBack, onStop }: Props) {
  const [user, setUser] = useState(username || '')
  const [password, setPassword] = useState('')
  const [save, setSave] = useState(true)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const id = useId()
  const ready = user.trim() !== '' && password !== ''
  return (
    <form
      className={`${compact ? 'caption ' : ''}ask login-form`}
      aria-label={`Sign in to ${site}`}
      autoComplete="off"
      onSubmit={async (e) => {
        e.preventDefault()
        if (!ready || busy) return
        setBusy(true)
        setErr('')
        try {
          await api.signIn(user, password, save)
          setPassword('')
        } catch (x) {
          setErr((x as Error).message)
        } finally {
          setBusy(false)
        }
      }}
    >
      {!compact && (
        <div className="eyebrow">
          <Lock size={14} />
          <span>
            Sign in to <span className="site-name">{site}</span>
          </span>
        </div>
      )}
      <p className="q">
        {compact && (
          <>
            Sign in to <span className="site-name">{site}</span>.{' '}
          </>
        )}
        {reason}
      </p>
      <div className="fields">
        <label htmlFor={`${id}-u`} className="sr">
          Username or email for {site}
        </label>
        <input id={`${id}-u`} type="text" placeholder="Username or email" autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={user} onChange={(e) => setUser(e.target.value)} {...noManagers} />
        <label htmlFor={`${id}-p`} className="sr">
          Password for {site}
        </label>
        <input id={`${id}-p`} type="password" placeholder="Password" autoComplete="off" value={password} onChange={(e) => setPassword(e.target.value)} {...noManagers} />
      </div>
      <label className="save">
        <button type="button" className="toggle" role="switch" aria-checked={save} onClick={() => setSave(!save)} />
        <span>Save for next time</span>
      </label>
      {err && <p className="err">{err}</p>}
      <div className="opts">
        <button type="submit" className="btn sm primary" disabled={!ready || busy}>
          {busy ? <span className="spinner" /> : 'Sign in'}
        </button>
        {control ? (
          <button type="button" className="btn sm" onClick={onHandBack}>
            <HandBack size={16} />
            <span>Hand back</span>
          </button>
        ) : (
          <button type="button" className="btn sm" onClick={onTakeOver}>
            <Pointer size={16} />
            <span>Sign in myself</span>
          </button>
        )}
        <button type="button" className="btn sm" onClick={onStop}>
          Not now
        </button>
      </div>
      <p className="hint">
        <Shield size={14} />
        <span>
          Alexa never sees your password. {save ? 'It’s saved encrypted on your server and only' : 'It’s only'} typed into {site}.
        </span>
      </p>
    </form>
  )
}
