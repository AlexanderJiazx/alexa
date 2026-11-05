import { createContext, useContext, useId, type ComponentProps, type ReactNode } from 'react'
import { cx } from './cx'

type FieldProps = {
  label: string
  // id ties the label to the control; one is made up if not given. The
  // control gets it through children as a function.
  id?: string
  help?: ReactNode
  error?: string
  // hideLabel keeps the label for screen readers only.
  hideLabel?: boolean
  flush?: boolean
  className?: string
  // children draws the control. Input, TextArea and Select pick up the
  // help or error under them on their own; a control of another kind sets
  // aria-describedby to describedBy (undefined when there's nothing).
  children: (id: string, describedBy?: string) => ReactNode
}

// What a Field tells the control it labels: its id, and the id of the line
// under it, so a screen reader reads the help or the error with the field.
type FieldInfo = { id: string; describedBy?: string; invalid: boolean }
const FieldCtx = createContext<FieldInfo | null>(null)

// Field is a labelled control with optional help or an error under it:
// <Field label="Name">{(id) => <Input id={id} … />}</Field>. The error is
// read with the field when it's focused; it isn't an alert of its own, so a
// form with three empty fields doesn't shout three "Required"s at once (the
// form says how many need an answer, once).
export function Field({ label, id, help, error, hideLabel, flush, className, children }: FieldProps) {
  const auto = useId()
  const fid = id ?? auto
  const msg = error || help ? `${fid}-msg` : undefined
  return (
    <div className={cx('field', flush && 'flush', className)}>
      <label htmlFor={fid} className={hideLabel ? 'sr' : undefined}>
        {label}
      </label>
      <FieldCtx.Provider value={{ id: fid, describedBy: msg, invalid: !!error }}>{children(fid, msg)}</FieldCtx.Provider>
      {error ? (
        <span id={msg} className="err">
          {error}
        </span>
      ) : (
        help && (
          <span id={msg} className="help">
            {help}
          </span>
        )
      )}
    </div>
  )
}

// described gives the control a Field labels the Field's help or error, and
// marks it invalid with an error, unless the control says otherwise.
function useDescribed(p: { id?: string; 'aria-describedby'?: string; 'aria-invalid'?: ComponentProps<'input'>['aria-invalid'] }) {
  const f = useContext(FieldCtx)
  if (!f || p.id !== f.id) return {}
  return {
    'aria-describedby': [p['aria-describedby'], f.describedBy].filter(Boolean).join(' ') || undefined,
    'aria-invalid': p['aria-invalid'] ?? (f.invalid || undefined),
  }
}

// Input, TextArea and Select are the controls, as wells on the surface.
// They take a ref like the element they draw (React 19 passes it as a
// prop).
export function Input({ className, ...p }: ComponentProps<'input'>) {
  return <input className={cx('input', className)} {...p} {...useDescribed(p)} />
}

export function TextArea({ className, ...p }: ComponentProps<'textarea'>) {
  return <textarea className={cx('input', className)} {...p} {...useDescribed(p)} />
}

export function Select({ className, ...p }: ComponentProps<'select'>) {
  return <select className={cx('input', className)} {...p} {...useDescribed(p)} />
}
