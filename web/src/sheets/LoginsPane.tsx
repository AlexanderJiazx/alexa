import { useId, useState } from 'react'
import { api } from '../api/client'
import type { Login } from '../api/types'
import { ago } from '../lib/format'
import { siteAddress } from '../lib/url'
import { useChanged } from '../state/selectors'
import { Button } from '../ui/Button'
import { ConfirmDialog } from '../ui/ConfirmDialog'
import { EmptyState } from '../ui/EmptyState'
import { Favicon } from '../ui/Favicon'
import { Field, Input } from '../ui/Field'
import { IconButton } from '../ui/IconButton'
import { KeyIcon, Shield, Trash } from '../ui/icons'
import { useIntent } from './intent'
import { failed, Group, HeadActions, LoadError, RowSkeletons, Section, useFirstFocus, useFocusBack, useList } from './parts'
import { notify } from './toasts'

// Password managers leave these fields alone: they're for a site Alexa
// signs in to, not for this app.
const noFill = { autoComplete: 'off', 'data-1p-ignore': '', 'data-lpignore': 'true' } as const

// LoginsPane is Settings' Logins: the logins Alexa can sign in with.
// Passwords go in and never come back out.
export function LoginsPane() {
  const changed = useChanged('logins')
  const { list, error, reload } = useList(api.logins, [changed])
  const intent = useIntent('logins')
  const [adding, setAdding] = useState(intent === 'new')
  const [changing, setChanging] = useState<number | null>(null)
  const [deleting, setDeleting] = useState<Login | null>(null)
  const ids = useId()
  const back = useFocusBack(list)

  return (
    <>
      {!adding && !!list?.length && (
        <HeadActions>
          <Button id={`${ids}add`} size="sm" onClick={() => setAdding(true)}>
            Add
          </Button>
        </HeadActions>
      )}
      {adding && (
        <AddLogin
          onDone={() => {
            setAdding(false)
            back([`${ids}add`])
            reload()
          }}
          onCancel={() => {
            setAdding(false)
            back([`${ids}add`, `${ids}empty`])
          }}
        />
      )}
      <Section title={adding && list?.length ? 'Saved' : undefined}>
        {error && !list ? (
          <LoadError message={error} onRetry={reload} />
        ) : list === null ? (
          <RowSkeletons count={2} />
        ) : list.length === 0 ? (
          !adding && (
            <EmptyState
              icon={<KeyIcon size={28} />}
              title="No saved logins"
              body="When Alexa needs to sign in somewhere, it asks you, and you can save the login for next time."
              action={
                <Button id={`${ids}empty`} variant="primary" onClick={() => setAdding(true)}>
                  Add a login
                </Button>
              }
            />
          )
        ) : (
          <Group>
            <ul className="row-list">
              {list.map((l) => (
                <li key={l.id} className="row login">
                  <span className="row-lead">
                    <Favicon host={l.site} size={20} />
                  </span>
                  <div className="row-text">
                    <div className="row-label">{l.site}</div>
                    <div className="row-sub">
                      {l.username} · <span className="num">{l.usedAt ? `used ${ago(l.usedAt)}` : `saved ${ago(l.updatedAt)}`}</span>
                    </div>
                    {changing === l.id && (
                      <ChangePassword
                        login={l}
                        onDone={() => {
                          setChanging(null)
                          back([`${ids}change${l.id}`])
                          reload()
                        }}
                        onCancel={() => {
                          setChanging(null)
                          back([`${ids}change${l.id}`])
                        }}
                      />
                    )}
                  </div>
                  {changing !== l.id && (
                    <div className="row-end">
                      <Button id={`${ids}change${l.id}`} size="sm" variant="ghost" onClick={() => setChanging(l.id)}>
                        Change password
                      </Button>
                      <IconButton size="sm" tone="muted" label={`Delete the login for ${l.site}`} onClick={() => setDeleting(l)}>
                        <Trash size={16} />
                      </IconButton>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </Group>
        )}
      </Section>
      <p className="foot-note">
        <Shield size={16} />
        <span>Passwords are encrypted on your server. They’re never shown to Alexa’s model or sent back to this app, and they’re only typed into the site they belong to.</span>
      </p>
      <ConfirmDialog
        open={!!deleting}
        title={deleting ? `Delete the login for ${deleting.site}?` : ''}
        body={deleting && `Alexa won’t be able to sign in to ${deleting.site} as ${deleting.username} any more. You can add it again later.`}
        confirmLabel="Delete"
        tone="danger"
        onCancel={() => setDeleting(null)}
        onConfirm={async () => {
          const l = deleting
          setDeleting(null)
          if (!l) return
          try {
            await api.deleteLogin(l.id)
            notify({ title: 'Login deleted', body: l.site, tone: 'ok' })
            reload()
          } catch (e) {
            failed('Couldn’t delete that login')(e as Error)
          }
        }}
      />
    </>
  )
}

function AddLogin({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const [form, setForm] = useState({ site: '', username: '', password: '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const first = useFirstFocus()
  return (
    <Section title="New login">
      <form
        autoComplete="off"
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          setBusy(true)
          try {
            // The server keeps the site as it signs in to it (its domain), so
            // the note says what the list will.
            const saved = await api.addLogin({ ...form, site: siteAddress(form.site) })
            notify({ title: 'Login saved', body: saved?.site || siteAddress(form.site), tone: 'ok' })
            onDone()
          } catch (x) {
            setErr((x as Error).message)
          } finally {
            setBusy(false)
          }
        }}
      >
        <Group pad>
          <Field label="Site" id={first} flush>
            {(id) => <Input id={id} required autoCapitalize="none" autoCorrect="off" spellCheck={false} value={form.site} onChange={(e) => setForm({ ...form, site: e.target.value })} placeholder="amazon.com" {...noFill} />}
          </Field>
          <div className="inline">
            <Field label="Username or email">
              {(id) => <Input id={id} required autoCapitalize="none" autoCorrect="off" spellCheck={false} value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} {...noFill} />}
            </Field>
            <Field label="Password">
              {(id) => <Input id={id} required type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} {...noFill} />}
            </Field>
          </div>
          {err && <p className="err">{err}</p>}
          <div className="row-btns end">
            <Button variant="ghost" onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={!form.site.trim() || !form.username.trim() || !form.password} loading={busy}>
              Save
            </Button>
          </div>
        </Group>
      </form>
    </Section>
  )
}

function ChangePassword({ login, onDone, onCancel }: { login: Login; onDone: () => void; onCancel: () => void }) {
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
          await api.updateLogin(login.id, { password })
          notify({ title: 'Password changed', body: login.site, tone: 'ok' })
          onDone()
        } catch (x) {
          failed('Couldn’t change the password')(x as Error)
        } finally {
          setBusy(false)
        }
      }}
    >
      <Field label={`New password for ${login.site}`} id={first} hideLabel flush>
        {(id) => <Input id={id} type="password" required placeholder="New password" value={password} onChange={(e) => setPassword(e.target.value)} {...noFill} />}
      </Field>
      <span className="row-form-btns">
        <Button size="sm" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" size="sm" variant="primary" disabled={!password} loading={busy}>
          Save
        </Button>
      </span>
    </form>
  )
}
