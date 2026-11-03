import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { cx } from './cx'
import { Close } from './icons'

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  icon?: ReactNode
  // selected marks a chip that's on, in a set of filters or choices.
  selected?: boolean
  // onRemove adds a small × that removes the chip.
  onRemove?: () => void
}

// Chip is a small pressable choice: a suggestion, a filter, an attachment.
export function Chip({ icon, selected, onRemove, className, children, type = 'button', ...rest }: Props) {
  const chip = (
    <button type={type} className={cx('chip', className)} aria-pressed={selected} {...rest}>
      {icon}
      <span className="chip-label">{children}</span>
    </button>
  )
  if (!onRemove) return chip
  return (
    <span className="chip-group">
      {chip}
      <button type="button" className="chip-x" aria-label="Remove" onClick={onRemove}>
        <Close size={12} />
      </button>
    </span>
  )
}
