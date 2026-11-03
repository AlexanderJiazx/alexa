import { useMemo, useState, type FormEvent } from 'react'
import type { AskForm as Form } from '../../api/blocks'
import { Button } from '../../ui/Button'
import { Check } from '../../ui/icons'
import { Kbd } from '../../ui/Kbd'
import { FormFieldInput } from './FormField'
import { check, initialValues, submitted, summary, validate, type Errors, type Value, type Values } from './form'
import { Enter } from './icons'
import type { Reply } from './reply'
import { useSentFocus } from './sentFocus'

type Props = {
  form: Form
  reply: Reply
  compact?: boolean
  // send gets the visible summary and the typed values.
  send: (text: string, values: Values) => void
}

// AskForm is a small native form. Errors show once a field has been left
// or the form submitted; Enter in any field submits. Once sent, the form
// gives way to a chip of what was sent.
export function AskForm({ form, reply, compact, send }: Props) {
  const fields = form.fields
  const [values, setValues] = useState<Values>(() => initialValues(fields))
  const [touched, setTouched] = useState<Record<string, boolean>>({})
  const [tried, setTried] = useState(false)
  const errors: Errors = useMemo(() => validate(fields, values), [fields, values])
  const off = reply.busy || reply.sent
  const { box, sent, before } = useSentFocus<HTMLFormElement>(off)
  const label = form.submit?.trim() || 'Send'

  const set = (name: string, v: Value) => setValues((s) => ({ ...s, [name]: v }))
  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (off) return
    setTried(true)
    const bad = fields.find((f) => errors[f.name])
    if (bad) {
      box.current?.querySelector<HTMLElement>(`[name="${bad.name}"]`)?.focus()
      return
    }
    const out = submitted(fields, values)
    before()
    send(summary(fields, out, label), out)
  }

  if (off)
    return (
      <p ref={sent} className="ask-sent" tabIndex={-1} aria-live="polite">
        {reply.busy ? <span className="spinner" aria-hidden="true" /> : <Check size={16} />}
        <span>{summary(fields, submitted(fields, values), label)}</span>
      </p>
    )

  return (
    <form ref={box} className={`ask-form${compact ? ' compact' : ''}`} noValidate onSubmit={onSubmit}>
      <div className="ask-fields">
        {fields.map((f) => (
          <FormFieldInput
            key={f.name}
            field={f}
            value={values[f.name]}
            error={tried || touched[f.name] ? check(f, values[f.name]) : ''}
            onChange={(v) => set(f.name, v)}
            onBlur={() => setTouched((t) => (t[f.name] ? t : { ...t, [f.name]: true }))}
          />
        ))}
      </div>
      <div className="ask-submit">
        <Button type="submit" variant="primary" size={compact ? 'sm' : 'md'}>
          {label}
        </Button>
        <span className="kbd-hint">
          <Kbd>
            <Enter size={12} />
          </Kbd>
        </span>
      </div>
    </form>
  )
}
