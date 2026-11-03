import { useId, useState } from 'react'
import { api } from '../api/client'
import type { User } from '../api/types'
import { ago } from '../lib/format'
import { useUser } from '../state/selectors'
import { Button } from '../ui/Button'
import { ConfirmDialog } from '../ui/ConfirmDialog'
import { Field, Input } from '../ui/Field'
import { IconButton } from '../ui/IconButton'
import { Trash } from '../ui/icons'
import { Pill } from '../ui/Pill'
import { Toggle } from '../ui/Toggle'
import { failed, Group, HeadActions, LoadError, RowSkeletons, Section, useFirstFocus, useFocusBack, useList } from './parts'
import { notify } from './toasts'

const blank = { username: '', password: '', displayName: '', isAdmin: false }

// UsersPane is Settings' Users, where an admin adds and removes the people
// who use Alexa: the people first, and Add in the head, as Logins and
// Scheduled have it. Deleting someone asks first; a new password is typed
// into a masked field, never shown (FE-13).
export function UsersPane() {
  const me = useUser() as User
  const { list, error, reload } = useList(api.users)
  const [adding, setAdding] = useState(false)
  const [resetting, setResetting] = useState<number | null>(null)
  const [deleting, setDeleting] = useState<User | null>(null)
  const ids = useId()
  const back = useFocusBack(list)
  return (
    <>
      {!adding && (
        <HeadActions>
          <Button id={`${ids}add`} size="sm" onClick={() => setAdding(true)}>
            Add
          </Button>
        </HeadActions>
      )}
      <p className="intro">Each person gets their own conversation with Alexa, memory, files and browser profile.</p>
      {adding && (
        <AddUser
          onDone={() => {
            setAdding(false)
            back([`${ids}add`])
            reload()
          }}
          onCancel={() => {
            setAdding(false)
            back([`${ids}add`])
          }}
        />
      )}
      <Section title="People">
        {error && !list ? (
          <LoadError message={error} onRetry={reload} />
        ) : list === null ? (
          <RowSkeletons count={2} />
        ) : (
          <Group>
            <ul className="row-list">
              {list.map((u) => (
                <li key={u.id} className="row person">
                  <span className="row-lead">
                    <span className="avatar" aria-hidden="true">
                      {(u.displayName || u.username).slice(0, 1).toUpperCase()}
                    </span>
                  </span>
                  <div className="row-text">
                    <div className="row-label">
                      {u.displayName || u.username}
                      {u.isAdmin && <Pill>Admin</Pill>}
                      {u.id === me.id && <Pill>You</Pill>}
                    </div>
                    <div className="row-sub">
                      @{u.username} · <span className="num">joined {ago(u.createdAt)}</span>
                    </div>
                    {resetting === u.id && (
                      <ResetPassword
                        user={u}
                        onDone={() => {
                          setResetting(null)
                          back([`${ids}reset${u.id}`])
                        }}
                        onCancel={() => {
                          setResetting(null)
                          back([`${ids}reset${u.id}`])
                        }}
                      />
                    )}
                  </div>
                  {u.id !== me.id && resetting !== u.id && (
                    <div className="row-end">
                      <Button id={`${ids}reset${u.id}`} size="sm" variant="ghost" onClick={() => setResetting(u.id)}>
                        Reset password
                      </Button>
                      <IconButton size="sm" tone="muted" label={`Delete ${u.username}`} onClick={() => setDeleting(u)}>
                        <Trash size={16} />
                      </IconButton>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </Group>
        )}
        {list?.length === 1 && !adding && <p className="help after">Just you so far. Add someone and they get an Alexa of their own.</p>}
      </Section>
      <ConfirmDialog
        open={!!deleting}
        title={deleting ? `Delete ${deleting.displayName || deleting.username}?` : ''}
        body="Their conversation, memory, files, logins, schedules and browser profile are deleted too. This can’t be undone."
        confirmLabel="Delete"
        tone="danger"
        onCancel={() => setDeleting(null)}
        onConfirm={async () => {
          const u = deleting
          setDeleting(null)
          if (!u) return
          try {
            await api.deleteUser(u.id)
            notify({ title: `${u.username} deleted`, tone: 'ok' })
            reload()
          } catch (e) {
            failed('Couldn’t delete them')(e as Error)
          }
        }}
      />
    </>
  )
}

function AddUser({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const [f, setF] = useState(blank)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const first = useFirstFocus()
  return (
    <Section title="Add someone">
      <form
        autoComplete="off"
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          setBusy(true)
          try {
            await api.createUser(f)
            notify({ title: `${f.username} can sign in now`, tone: 'ok' })
            onDone()
          } catch (x) {
            setErr((x as Error).message)
          } finally {
            setBusy(false)
          }
        }}
      >
        <Group pad>
          <div className="inline">
            <Field label="Username" id={first} flush>
              {(id) => <Input id={id} required autoCapitalize="none" autoCorrect="off" spellCheck={false} value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} autoComplete="off" />}
            </Field>
            <Field label="Password" flush help="At least 8 characters.">
              {(id) => <Input id={id} required type="password" minLength={8} value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} autoComplete="new-password" />}
            </Field>
          </div>
          <Field label="Display name">{(id) => <Input id={id} value={f.displayName} onChange={(e) => setF({ ...f, displayName: e.target.value })} autoComplete="off" />}</Field>
          <label className="toggle-row admin-row">
            <Toggle checked={f.isAdmin} label="Admin" onChange={(isAdmin) => setF({ ...f, isAdmin })} />
            <span className="admin-text">
              Admin
              <span className="help">Can add and remove people.</span>
            </span>
          </label>
          {err && <p className="err">{err}</p>}
          <div className="row-btns end">
            <Button variant="ghost" onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" loading={busy}>
              Add
            </Button>
          </div>
        </Group>
      </form>
    </Section>
  )
}

// ResetPassword sets someone's new password in a masked field: an admin
// never sees a password in plain text, and neither does anyone behind them.
function ResetPassword({ user, onDone, onCancel }: { user: User; onDone: () => void; onCancel: () => void }) {
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const first = useFirstFocus()
  return (
    <form
      className="row-form"
      autoComplete="off"
      onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        try {
          await api.resetPassword(user.id, password)
          notify({ title: 'Password reset', body: `${user.username} signs in with the new one from now on.`, tone: 'ok' })
          onDone()
        } catch (x) {
          failed('Couldn’t reset the password')(x as Error)
        } finally {
          setBusy(false)
        }
      }}
    >
      <Field label={`New password for ${user.username}`} id={first} hideLabel flush>
        {(id) => <Input id={id} type="password" required minLength={8} placeholder="New password, 8 or more" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />}
      </Field>
      <span className="row-form-btns">
        <Button size="sm" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" size="sm" variant="primary" loading={busy} disabled={password.length < 8}>
          Set
        </Button>
      </span>
    </form>
  )
}
