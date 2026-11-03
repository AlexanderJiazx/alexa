import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { cx } from './cx'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'lg'

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
  // block stretches the button to its container's width.
  block?: boolean
  // loading shows a spinner in place of the label and blocks presses.
  loading?: boolean
  icon?: ReactNode
}

// buttonClass is the class list for a button-looking element. Use it on an
// <a> or a <label> that should look like a button.
export function buttonClass(variant: ButtonVariant = 'secondary', size: ButtonSize = 'md', block = false) {
  return cx('btn', variant !== 'secondary' && variant, size !== 'md' && size, block && 'block')
}

// Button is the one button: primary (ink, one per view), secondary (the
// default), ghost (no frame) and danger. Sizes are 32, 40 and 48px.
export function Button({ variant = 'secondary', size = 'md', block, loading, icon, className, children, disabled, type = 'button', ...rest }: Props) {
  return (
    <button type={type} className={cx(buttonClass(variant, size, block), className)} disabled={disabled || loading} aria-busy={loading || undefined} {...rest}>
      {loading ? <span className="spinner" aria-hidden="true" /> : icon}
      {loading ? <span className="sr">{children}</span> : children}
    </button>
  )
}
