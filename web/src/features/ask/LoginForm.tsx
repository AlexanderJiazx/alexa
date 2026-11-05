import { useId, useState } from 'react'
import { api } from '../../api/client'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { Field, Input } from '../../ui/Field'
import { HandBack, Lock, Pointer, Shield } from '../../ui/icons'
import { Toggle } from '../../ui/Toggle'
import { NeedsFooter } from './NeedsFooter'
import { useReply } from './reply'
import './ask.css'

type Props = {
  site: string
  username?: string
  reason: string
  // compact is the version in the caption under the browser on small
  // screens; dock is the one in the needs-you dock.
  compact?: boolean
  dock?: boolean
  control: boolean
  onTakeOver: () => void
  onHandBack: () => void
  // onStop is kept for callers that pass it; Stop is the footer's.
  onStop?: () => void
}

// Password managers would offer this app's own login here, which is never
// what's wanted.
const noManagers = { 'data-1p-ignore': '', 'data-lpignore': 'true', 'data-bwignore': '', 'data-form-type': 'other' }

const skipText = 'Skip signing in for now. Carry on without it.'

// LoginForm is how Alexa asks to sign in to a site. What the user types
// goes straight to the server, which saves it encrypted (if they like) and
// types it into that site only. It never enters the conversation, and
// Alexa's model never sees the password.
export function LoginForm({ site, username, reason, compact, dock, control, onTakeOver, onHandBack }: Props) {
  const [user, setUser] = useState(username || '')
  const [password, setPassword] = useState('')
  const [save, setSave] = useState(true)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const skip = useReply()
  const id = useId()
  const small = compact || dock
  const ready = user.trim() !== '' && password !== ''
  return (
    <form
      className={cx(compact && 'caption', 'ask login-form', small && 'compact')}
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
      {!small && (
        <div className="eyebrow">
          <Lock size={14} />
          <span>
            Sign in to <span className="site-name">{site}</span>
          </span>
        </div>
      )}
      <p className="q">
        {small && (
          <>
            Sign in to <span className="site-name">{site}</span>.{' '}
          </>
        )}
        {reason}
      </p>
      <div className="fields">
        <Field label={`Username or email for ${site}`} hideLabel flush id={`${id}-u`}>
          {(fid) => <Input id={fid} type="text" placeholder="Username or email" autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={user} onChange={(e) => setUser(e.target.value)} {...noManagers} />}
        </Field>
        <Field label={`Password for ${site}`} hideLabel flush id={`${id}-p`}>
          {(fid) => <Input id={fid} type="password" placeholder="Password" autoComplete="off" value={password} onChange={(e) => setPassword(e.target.value)} {...noManagers} />}
        </Field>
      </div>
      <div className="save toggle-row">
        <Toggle label="Save for next time" checked={save} onChange={setSave} />
        <span aria-hidden="true">Save for next time</span>
      </div>
      {err && (
        <p className="err" role="alert">
          {err}
        </p>
      )}
      <div className="opts">
        <Button type="submit" variant="primary" size={small ? 'sm' : 'md'} disabled={!ready} loading={busy}>
          Sign in
        </Button>
        {control ? (
          <Button size={small ? 'sm' : 'md'} icon={<HandBack size={16} />} onClick={onHandBack}>
            Hand back
          </Button>
        ) : (
          <Button size={small ? 'sm' : 'md'} icon={<Pointer size={16} />} onClick={onTakeOver}>
            Sign in myself
          </Button>
        )}
      </div>
      {!small && (
        <p className="hint">
          <Shield size={14} />
          <span>
            Alexa never sees your password. {save ? 'It’s saved encrypted on your server and only' : 'It’s only'} typed into {site}.
          </span>
        </p>
      )}
      {skip.error && (
        <p className="ask-err" role="alert">
          <span>Couldn’t send</span>
          <span aria-hidden="true">·</span>
          <button type="button" className="linkish" onClick={skip.retry}>
            Retry
          </button>
        </p>
      )}
      <NeedsFooter onSkip={() => skip.send({ text: skipText })} off={skip.busy || skip.sent} />
    </form>
  )
}
