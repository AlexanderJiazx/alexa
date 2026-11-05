import { cx } from './cx'

type Props = {
  checked: boolean
  onChange: (on: boolean) => void
  label: string
  disabled?: boolean
  className?: string
}

// Toggle is an on/off switch. Give it a visible label next to it, or rely
// on label for screen readers.
export function Toggle({ checked, onChange, label, disabled, className }: Props) {
  return <button type="button" role="switch" className={cx('toggle', className)} aria-checked={checked} aria-label={label} disabled={disabled} onClick={() => onChange(!checked)} />
}
