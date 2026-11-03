import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { cx } from './cx'

type Props = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'aria-label'> & {
  // label names the button for screen readers and as its tooltip.
  label: string
  size?: 'sm' | 'md'
  tone?: 'default' | 'muted'
  children: ReactNode
}

// IconButton is a button that is only a glyph: 44px (36px small), a quiet
// hover wash, and always a label.
export function IconButton({ label, size = 'md', tone = 'default', className, children, type = 'button', title, ...rest }: Props) {
  return (
    <button type={type} className={cx('icon-btn', size === 'sm' && 'sm', tone === 'muted' && 'muted', className)} aria-label={label} title={title ?? label} {...rest}>
      {children}
    </button>
  )
}
