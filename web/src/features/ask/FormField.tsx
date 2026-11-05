import { useId } from 'react'
import type { FormField } from '../../api/blocks'
import { Chip } from '../../ui/Chip'
import { cx } from '../../ui/cx'
import { Field, Input, Select } from '../../ui/Field'
import { IconButton } from '../../ui/IconButton'
import { Check, Plus } from '../../ui/icons'
import { Toggle } from '../../ui/Toggle'
import { atMax, atMin, hint, step, type Value } from './form'
import { Minus } from './icons'

type Props = {
  field: FormField
  value: Value | undefined
  error: string
  onChange: (v: Value) => void
  onBlur: () => void
}

const inputType: Partial<Record<FormField['type'], string>> = { date: 'date', time: 'time', datetime: 'datetime-local', number: 'number' }

// Wide fields take the form's whole row; the rest sit two to a row where
// there's room.
const wide = (f: FormField) => f.type === 'text' || f.type === 'multiselect' || f.type === 'toggle'

// A field that can be left empty says so; the rest are needed. Toggles and
// steppers always hold a value, so they never need the word.
const label = (f: FormField) => (f.required || f.type === 'toggle' || f.type === 'stepper' ? f.label : `${f.label} (optional)`)

// FormFieldInput draws one field of an ask's form with the native control
// for its type.
export function FormFieldInput({ field: f, value, error, onChange, onBlur }: Props) {
  const id = useId()
  const cls = cx('ask-field', wide(f) && 'wide')

  if (f.type === 'toggle')
    return (
      <div className={cx(cls, 'toggle-row')}>
        <span id={id} className="ask-toggle-label">
          {f.label}
        </span>
        <Toggle label={f.label} checked={value === true} onChange={onChange} />
      </div>
    )

  if (f.type === 'multiselect') {
    const list = Array.isArray(value) ? value : []
    return (
      <fieldset className={cls} aria-describedby={error ? `${id}-e` : undefined}>
        <legend className="ask-legend">{label(f)}</legend>
        <div className="ask-chips">
          {(f.options ?? []).map((o) => {
            const on = list.includes(o)
            return (
              <Chip key={o} name={f.name} selected={on} icon={on ? <Check size={14} /> : undefined} onClick={() => onChange(on ? list.filter((x) => x !== o) : [...list, o])} onBlur={onBlur}>
                {o}
              </Chip>
            )
          })}
        </div>
        {error && (
          <span id={`${id}-e`} className="err" role="alert">
            {error}
          </span>
        )}
      </fieldset>
    )
  }

  if (f.type === 'stepper') {
    const n = typeof value === 'number' ? value : (f.min ?? 0)
    return (
      <div className={cls}>
        <span className="ask-legend" id={id}>
          {f.label}
        </span>
        <div className="ask-stepper" role="group" aria-labelledby={id}>
          <IconButton label={`Fewer ${f.label.toLowerCase()}`} size="sm" disabled={atMin(f, n)} onClick={() => onChange(step(f, n, -1))}>
            <Minus size={16} />
          </IconButton>
          <output className="num" aria-live="polite" aria-labelledby={id}>
            {n}
          </output>
          <IconButton label={`More ${f.label.toLowerCase()}`} size="sm" disabled={atMax(f, n)} onClick={() => onChange(step(f, n, 1))}>
            <Plus size={16} />
          </IconButton>
        </div>
      </div>
    )
  }

  return (
    <Field label={label(f)} error={error || undefined} className={cls} flush>
      {(fid) =>
        f.type === 'select' ? (
          <Select id={fid} name={f.name} value={typeof value === 'string' ? value : ''} required={f.required} aria-invalid={!!error || undefined} onChange={(e) => onChange(e.target.value)} onBlur={onBlur}>
            {(!f.required || !value) && <option value="">{f.placeholder || 'Choose…'}</option>}
            {(f.options ?? []).map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </Select>
        ) : (
          <Input
            id={fid}
            name={f.name}
            type={inputType[f.type] ?? 'text'}
            inputMode={f.type === 'number' ? 'decimal' : undefined}
            className={f.type === 'number' ? 'num' : undefined}
            value={typeof value === 'string' ? value : value === undefined ? '' : String(value)}
            placeholder={hint(f.placeholder)}
            required={f.required}
            min={f.min}
            max={f.max}
            step={f.step}
            autoComplete="off"
            aria-invalid={!!error || undefined}
            onChange={(e) => onChange(e.target.value)}
            onBlur={onBlur}
          />
        )
      }
    </Field>
  )
}
