import { useEffect, useState, type ReactNode } from 'react'
import { cx } from './cx'
import { Alert, Check, Close, Info } from './icons'

export type ToastTone = 'neutral' | 'ok' | 'bad'

type Props = {
  title: string
  body?: string
  tone?: ToastTone
  // action is one small button: "Undo", "Open".
  action?: ReactNode
  onClose?: () => void
  leaving?: boolean
  onMouseEnter?: () => void
  onMouseLeave?: () => void
}

const icons: Record<ToastTone, ReactNode> = {
  neutral: <Info size={16} />,
  ok: <Check size={16} />,
  bad: <Alert size={16} />,
}

// Toast is a brief note, bottom centre above the composer, on the surface.
// The owner times it out (pausing while hovered) and sets leaving for the
// exit. Every toast has the same columns, an icon (a quiet one for a
// neutral note) then the words then its buttons, so a stack lines up.
export function Toast({ title, body, tone = 'neutral', action, onClose, leaving, onMouseEnter, onMouseLeave }: Props) {
  return (
    <div className={cx('toast', tone !== 'neutral' && tone, leaving && 'leaving')} onMouseEnter={onMouseEnter} onMouseLeave={onMouseLeave}>
      <Say text={body ? `${title}. ${body}` : title} alert={tone === 'bad'} />
      <span className="toast-ic" aria-hidden="true">
        {icons[tone]}
      </span>
      <div className="toast-text">
        <b>{title}</b>
        {body && <span className="b">{body}</span>}
      </div>
      {action}
      {onClose && (
        <button type="button" className="icon-btn sm muted toast-x" aria-label="Dismiss" onClick={onClose}>
          <Close size={14} />
        </button>
      )}
    </div>
  )
}

// Say reads a toast out. A live region that arrives already filled in is
// often not read (NVDA, JAWS), so it arrives empty and the words follow a
// moment later. The words on screen aren't live, so nothing is read twice.
function Say({ text, alert }: { text: string; alert: boolean }) {
  const [said, setSaid] = useState('')
  useEffect(() => {
    const t = setTimeout(() => setSaid(text), 100)
    return () => clearTimeout(t)
  }, [text])
  return (
    <span className="sr" role={alert ? 'alert' : 'status'}>
      {said}
    </span>
  )
}

// ToastStack holds the toasts, newest nearest the composer.
export function ToastStack({ children }: { children: ReactNode }) {
  return <div className="toasts">{children}</div>
}
