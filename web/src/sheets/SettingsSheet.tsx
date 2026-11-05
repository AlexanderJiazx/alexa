import { useMemo, useState } from 'react'
import { api } from '../api/client'
import type { User } from '../api/types'
import { openSettings, setUser, signOut } from '../state/actions'
import { useConfig, useSession, useUser } from '../state/selectors'
import { Button } from '../ui/Button'
import { Field, Input, Select, TextArea } from '../ui/Field'
import { Pill } from '../ui/Pill'
import { Appearance } from './Appearance'
import { useIntent, wantIntent } from './intent'
import { Group, Row, Section, useFirstFocus, useSettingId } from './parts'
import { enablePush } from './push'
import { notify } from './toasts'
import { zoneOptions } from './zones'

// Settings' own sections: General (the profile, the theme), Notifications
// on this device, and Account (the password, signing out). The modal that
// shows them is Settings.tsx.

const zones = (() => {
  try {
    return (Intl as unknown as { supportedValuesOf: (k: string) => string[] }).supportedValuesOf('timeZone')
  } catch {
    return []
  }
})()

// openAboutYou opens Settings at General with About you in focus: the home
// checklist's "Tell Alexa where you live" says to write it there.
export function openAboutYou() {
  wantIntent('settings', 'about')
  openSettings('general')
}

// GeneralPane is the profile, then how the app looks; for an admin, what
// the server runs as a quiet line at the foot.
export function GeneralPane() {
  const user = useUser() as User
  const config = useConfig()
  const tokens = useSession((s) => s.contextTokens)
  return (
    <>
      <Profile user={user} emailEnabled={!!config?.emailEnabled} />
      <Appearance />
      {/* What the server runs, and how long the conversation is in tokens,
          are for whoever runs it. */}
      {config && user.isAdmin && (
        <p className="foot num">
          Model {config.model} · browser {config.viewport.width}×{config.viewport.height}
          {config.sandboxed ? ' · per-user sandbox' : ''}
          {config.shellEnabled ? ' · shell tool on' : ''}
          {tokens >= 1000 ? ` · conversation ${Math.round(tokens / 1000)}k tokens` : ''}
        </p>
      )}
    </>
  )
}

type ProfileForm = { displayName: string; email: string; timezone: string; about: string }
const fields = ['displayName', 'email', 'timezone', 'about'] as const
const formOf = (u: User): ProfileForm => ({ displayName: u.displayName, email: u.email, timezone: u.timezone, about: u.about })

// followUser keeps the profile form in step with the user when they change
// elsewhere (another device, changed:me; FH-14). base is what the form was
// filled from. A field nobody has touched here takes the new value; one
// being edited keeps the edit. So the form isn't left looking edited with
// old values, and Save doesn't undo the other change.
export function followUser(f: ProfileForm, base: ProfileForm, now: ProfileForm): ProfileForm {
  const next = { ...f }
  for (const k of fields) if (f[k] === base[k]) next[k] = now[k]
  return next
}

function Profile({ user, emailEnabled }: { user: User; emailEnabled: boolean }) {
  const [f, setF] = useState(() => formOf(user))
  const [base, setBase] = useState(() => formOf(user))
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const now = formOf(user)
  if (fields.some((k) => base[k] !== now[k])) {
    setF(followUser(f, base, now))
    setBase(now)
  }
  const dirty = fields.some((k) => f[k] !== base[k])
  // Places by name, "Los Angeles (GMT−7)"; the IANA id is only the value.
  const options = useMemo(() => zoneOptions(zones.includes(f.timezone) || !f.timezone ? zones : [f.timezone, ...zones]), [f.timezone])
  const about = useIntent('settings') === 'about'
  const aboutId = useFirstFocus(about, useSettingId('about'))
  const ids = { name: useSettingId('name'), timezone: useSettingId('timezone'), email: useSettingId('email') }
  return (
    <Section title="Profile">
      <form
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          setBusy(true)
          try {
            const saved = await api.updateMe(f)
            setUser(saved)
            setF(formOf(saved))
            setBase(formOf(saved))
            notify({ title: 'Profile saved', tone: 'ok' })
          } catch (x) {
            setErr((x as Error).message)
          } finally {
            setBusy(false)
          }
        }}
      >
        <Group pad>
          <div className="inline">
            <Field label="Name" id={ids.name} flush>
              {(id) => <Input id={id} value={f.displayName} onChange={(e) => setF({ ...f, displayName: e.target.value })} autoComplete="name" />}
            </Field>
            <Field label="Timezone" id={ids.timezone} flush>
              {(id) => (
                <Select id={id} value={f.timezone} onChange={(e) => setF({ ...f, timezone: e.target.value })}>
                  {options.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          </div>
          {/* Without email on the server the field would take an address
              for mail that never comes: it's left out, or, for an address
              saved already, kept (to correct or clear) with why it's unused. */}
          {(emailEnabled || !!user.email) && (
            <Field label="Email for notifications" id={ids.email} help={emailEnabled ? 'Alexa can email you when you ask it to.' : 'Email isn’t set up on this server, so Alexa can’t use this yet.'}>
              {(id) => <Input id={id} type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} placeholder="you@example.com" autoComplete="email" />}
            </Field>
          )}
          <Field label="About you" id={aboutId} help="Alexa keeps this in mind in every conversation.">
            {(id) => <TextArea id={id} rows={3} value={f.about} onChange={(e) => setF({ ...f, about: e.target.value })} placeholder="I live in …, I work as …, I prefer …" />}
          </Field>
          {err && <p className="err">{err}</p>}
          <div className="row-btns end">
            <Button type="submit" variant="primary" loading={busy} disabled={!dirty}>
              Save
            </Button>
          </div>
        </Group>
      </form>
    </Section>
  )
}

// PushPane turns on notifications for this device. What arrived while
// you were away is the Notifications inbox, under the avatar.
export function PushPane() {
  const config = useConfig()
  const supported = typeof Notification !== 'undefined'
  const [on, setOn] = useState(supported && Notification.permission === 'granted')
  const [busy, setBusy] = useState(false)
  return (
    <Section>
      <Group>
        <Row id={useSettingId('push')} label="On this device" sub="When Alexa says something while you’re away, it arrives as a notification here.">
          {on && <Pill tone="ok">On</Pill>}
          <Button
            size="sm"
            loading={busy}
            onClick={async () => {
              setBusy(true)
              try {
                if (!config) throw new Error('Not ready yet. Try again in a moment.')
                await enablePush(config)
                setOn(true)
                notify({ title: 'Notifications are on', body: 'For this device.', tone: 'ok' })
              } catch (x) {
                notify({ title: 'Couldn’t turn on notifications', body: (x as Error).message, tone: 'bad' })
              } finally {
                setBusy(false)
              }
            }}
          >
            {on ? 'Re-register' : 'Turn on'}
          </Button>
        </Row>
      </Group>
    </Section>
  )
}

// AccountPane changes the password, and signs out.
export function AccountPane() {
  const user = useUser() as User
  return (
    <>
      <Password />
      <Section>
        <Group>
          <Row id={useSettingId('signout')} label="Sign out" sub={`Signed in as ${user.displayName || user.username} (@${user.username}).`}>
            <Button size="sm" onClick={signOut}>
              Sign out
            </Button>
          </Row>
        </Group>
      </Section>
    </>
  )
}

function Password() {
  const [pw, setPw] = useState({ current: '', next: '' })
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  return (
    <Section title="Password">
      <form
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          setBusy(true)
          try {
            await api.changePassword(pw.current, pw.next)
            setPw({ current: '', next: '' })
            notify({ title: 'Password changed', tone: 'ok' })
          } catch (x) {
            setErr((x as Error).message)
          } finally {
            setBusy(false)
          }
        }}
      >
        <Group pad id={useSettingId('password')}>
          <div className="inline">
            <Field label="Current password" flush>
              {(id) => <Input id={id} type="password" autoComplete="current-password" value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} />}
            </Field>
            <Field label="New password" flush help="At least 8 characters.">
              {(id) => <Input id={id} type="password" autoComplete="new-password" minLength={8} value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} />}
            </Field>
          </div>
          {err && <p className="err">{err}</p>}
          <div className="row-btns end">
            <Button type="submit" variant="primary" disabled={!pw.current || pw.next.length < 8} loading={busy}>
              Change password
            </Button>
          </div>
        </Group>
      </form>
    </Section>
  )
}
