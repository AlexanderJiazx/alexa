import { useRef, type KeyboardEvent } from 'react'
import { cx } from './cx'
import { moveIndex } from './focus'

type Props<T extends string> = {
  value: T
  options: readonly (readonly [T, string])[]
  onChange: (v: T) => void
  label: string
  // tab makes it a tab list (role tab, aria-selected) rather than a radio
  // group; it looks the same.
  kind?: 'radio' | 'tab'
  size?: 'sm' | 'md'
  // block stretches it, with equal segments.
  block?: boolean
  className?: string
}

// Segmented is a small set of mutually exclusive choices on a sunken track,
// the chosen one raised. Arrow keys, Home and End move the choice, as in a
// native radio group; only the chosen segment is in the Tab order.
export function Segmented<T extends string>({ value, options, onChange, label, kind = 'radio', size = 'md', block, className }: Props<T>) {
  const ref = useRef<HTMLDivElement>(null)
  const tab = kind === 'tab'
  const onKeyDown = (e: KeyboardEvent) => {
    const i = moveIndex(e.key, options.findIndex(([v]) => v === value), options.length, 'horizontal')
    if (i === null) return
    e.preventDefault()
    onChange(options[i][0])
    ref.current?.querySelectorAll<HTMLElement>('button')[i]?.focus()
  }
  return (
    <div ref={ref} className={cx('seg', size === 'sm' && 'sm', block && 'block', className)} role={tab ? 'tablist' : 'radiogroup'} aria-label={label} onKeyDown={onKeyDown}>
      {options.map(([v, text]) => {
        const on = v === value
        return (
          <button key={v} type="button" role={tab ? 'tab' : 'radio'} aria-selected={tab ? on : undefined} aria-checked={tab ? undefined : on} tabIndex={on ? 0 : -1} onClick={() => onChange(v)}>
            {text}
          </button>
        )
      })}
    </div>
  )
}

// Tabs is Segmented as a tab list: for switching views (Reflect's tabs).
export function Tabs<T extends string>(p: Omit<Props<T>, 'kind'>) {
  return <Segmented {...p} kind="tab" />
}
