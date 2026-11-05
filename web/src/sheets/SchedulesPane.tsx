import { useId, useState } from 'react'
import { api } from '../api/client'
import type { Schedule } from '../api/types'
import { firstLine } from '../features/palette/context'
import { ago } from '../lib/format'
import { useChanged, useUser } from '../state/selectors'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { Field, Input, Select, TextArea } from '../ui/Field'
import { cx } from '../ui/cx'
import { IconButton } from '../ui/IconButton'
import { Clock, Trash } from '../ui/icons'
import { Pill } from '../ui/Pill'
import { Segmented } from '../ui/Segmented'
import { Toggle } from '../ui/Toggle'
import { dayNames, describeCron, fmtRun, freqs, fromCron, inSentence, nameFrom, namedByPrompt, toCron, zoneNote, type When } from './cron'
import { useIntent } from './intent'
import { failed, Group, HeadActions, LoadError, RowSkeletons, Section, useFirstFocus, useFocusBack, useList } from './parts'
import { notify } from './toasts'
import { removeLater, usePending } from './undo'

const blank = { name: '', prompt: '' }
const start: When = fromCron('0 8 * * *')

// SchedulesPane is Settings' Scheduled: what Alexa does on a schedule, and
// setting up more without cron for the usual cases.
export function SchedulesPane() {
  const changed = useChanged('schedules')
  const { list, error, reload } = useList(api.schedules, [changed])
  const pending = usePending()
  const intent = useIntent('schedules')
  const [adding, setAdding] = useState(intent === 'new')
  const shown = (list ?? []).filter((s) => !pending.has(`schedule:${s.id}`))
  const ids = useId()
  const back = useFocusBack(list)

  const remove = (s: Schedule) =>
    removeLater({
      key: `schedule:${s.id}`,
      title: 'Schedule deleted',
      body: namedByPrompt(s.name, s.prompt) ? firstLine(s.prompt, 48) : s.name,
      commit: () => api.deleteSchedule(s.id),
      done: reload,
      failed: failed('Couldn’t delete that schedule'),
    })

  return (
    <>
      {/* With nothing scheduled, the empty state carries the button. */}
      {!adding && shown.length > 0 && (
        <HeadActions>
          <Button id={`${ids}new`} size="sm" onClick={() => setAdding(true)}>
            Add
          </Button>
        </HeadActions>
      )}
      {adding && (
        <NewSchedule
          onDone={() => {
            setAdding(false)
            back([`${ids}new`])
            reload()
          }}
          onCancel={() => {
            setAdding(false)
            back([`${ids}new`, `${ids}empty`])
          }}
        />
      )}
      <Section title={adding && shown.length ? 'Your schedules' : undefined}>
        {error && !list ? (
          <LoadError message={error} onRetry={reload} />
        ) : list === null ? (
          <RowSkeletons count={2} />
        ) : shown.length === 0 ? (
          !adding && (
            <EmptyState
              icon={<Clock size={28} />}
              title="Nothing scheduled"
              body="Ask Alexa to do something “every morning” or “tomorrow at 9”, or set one up here. When one comes due, Alexa picks it up in your conversation."
              action={
                <Button id={`${ids}empty`} variant="primary" onClick={() => setAdding(true)}>
                  Schedule something
                </Button>
              }
            />
          )
        ) : (
          <Group>
            <ul className="row-list">
              {shown.map((s) => (
                <ScheduleRow key={s.id} s={s} onChange={reload} onDelete={() => remove(s)} />
              ))}
            </ul>
          </Group>
        )}
      </Section>
    </>
  )
}

function ScheduleRow({ s, onChange, onDelete }: { s: Schedule; onChange: () => void; onDelete: () => void }) {
  const [running, setRunning] = useState(false)
  // Times are the user's, as the server runs them, not this device's.
  const tz = useUser()?.timezone
  const when = [s.cron ? describeCron(s.cron) : `Once, ${inSentence(fmtRun(s.runAt, tz))}`, zoneNote(tz)].filter(Boolean).join(' · ')
  // A name that's only the start of the prompt would say it twice: the
  // prompt is the title then.
  const unnamed = namedByPrompt(s.name, s.prompt)
  // A one-off's line above already says when it runs.
  const next = s.enabled && !!s.nextRun && !!s.cron
  return (
    <li className="row schedule">
      <div className="row-text">
        <div className="row-label">
          <span className={cx(unnamed && 'clamp')}>{unnamed ? s.prompt : s.name}</span>
          {!s.enabled && <Pill>Paused</Pill>}
        </div>
        <div className="row-sub strong">{when}</div>
        {!unnamed && <div className="row-sub clamp">{s.prompt}</div>}
        <div className="row-meta num">
          {next ? `Next run ${inSentence(fmtRun(s.nextRun, tz))}` : s.enabled ? '' : 'Won’t run until you turn it on'}
          {s.lastRun ? `${next ? ' · ' : ''}Last ran ${ago(s.lastRun)}` : ''}
        </div>
        <div className="row-actions">
          <Button
            size="sm"
            variant="ghost"
            loading={running}
            onClick={async () => {
              setRunning(true)
              try {
                await api.runSchedule(s.id)
                notify({ title: `Running ${s.name}`, body: 'It’s in your conversation now.', tone: 'ok' })
                onChange()
              } catch (e) {
                failed('Couldn’t run that')(e as Error)
              } finally {
                setRunning(false)
              }
            }}
          >
            Run now
          </Button>
        </div>
      </div>
      <div className="row-end top">
        <Toggle
          checked={s.enabled}
          label={`${s.name} on`}
          onChange={async (on) => {
            try {
              await api.setScheduleEnabled(s.id, on)
              onChange()
            } catch (e) {
              failed('Couldn’t change that')(e as Error)
            }
          }}
        />
        <IconButton size="sm" tone="muted" label={`Delete ${s.name}`} onClick={onDelete}>
          <Trash size={16} />
        </IconButton>
      </div>
    </li>
  )
}

// NewSchedule is the form for a new schedule: what to do, and when, picked
// from the usual shapes (cron only under Custom).
export function NewSchedule({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const [form, setForm] = useState(blank)
  const [when, setWhen] = useState<When>(start)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const first = useFirstFocus()
  const cron = toCron(when)
  // The server runs it on the profile's clock, which says so when that
  // isn't this device's.
  const zone = zoneNote(useUser()?.timezone)
  return (
    <Section title="New schedule">
      <form
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          setBusy(true)
          try {
            const made = await api.createSchedule({ ...form, name: form.name.trim() || nameFrom(form.prompt), cron })
            notify({ title: 'Scheduled', body: describeCron(made?.cron || cron), tone: 'ok' })
            onDone()
          } catch (x) {
            setErr((x as Error).message)
          } finally {
            setBusy(false)
          }
        }}
      >
        <Group pad>
          <Field label="What should Alexa do?" id={first} flush>
            {(id) => <TextArea id={id} required rows={3} value={form.prompt} onChange={(e) => setForm({ ...form, prompt: e.target.value })} placeholder="Check my favourite news sites and send me a five-bullet briefing" />}
          </Field>
          <Field label="Name" help="Optional. Shown in your conversation when it runs.">
            {(id) => <Input id={id} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Morning briefing" />}
          </Field>
          <div className="field">
            <span className="field-label" aria-hidden="true">
              When
            </span>
            <Segmented label="How often" value={when.freq} options={freqs} onChange={(freq) => setWhen({ ...when, freq, cron: freq === 'custom' ? cron : when.cron })} block size="sm" />
          </div>
          {when.freq === 'custom' ? (
            <Field label="Cron" help={`${describeCron(when.cron)}, ${zone || 'in your timezone'}.`}>
              {(id) => <Input id={id} required className="mono" value={when.cron} onChange={(e) => setWhen({ ...when, cron: e.target.value })} spellCheck={false} autoCapitalize="none" />}
            </Field>
          ) : (
            <div className="inline">
              {when.freq === 'weekly' && (
                <Field label="Day">
                  {(id) => (
                    <Select id={id} value={when.day} onChange={(e) => setWhen({ ...when, day: Number(e.target.value) })}>
                      {dayNames.map((d, i) => (
                        <option key={d} value={i}>
                          {d}
                        </option>
                      ))}
                    </Select>
                  )}
                </Field>
              )}
              <Field label="Time" help={zone ? `${zone}, as set in Settings.` : 'In your timezone.'}>
                {(id) => <Input id={id} type="time" required value={when.time} onChange={(e) => setWhen({ ...when, time: e.target.value })} />}
              </Field>
            </div>
          )}
          <p className="preview num" aria-live="polite">
            {describeCron(cron)}
          </p>
          {err && <p className="err">{err}</p>}
          <div className="row-btns end">
            <Button variant="ghost" onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!form.prompt.trim()}>
              Save
            </Button>
          </div>
        </Group>
      </form>
      <p className="help tip">You can also just ask Alexa, like “every weekday at 8, summarise my inbox”.</p>
    </Section>
  )
}
