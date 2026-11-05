import { dismissPending, showPending, surfaceOf, useFollow } from '../../state/exchange'
import { useApp } from '../../state/store'
import { Close, Report } from '../../ui/icons'
import { Mark } from '../../ui/Mark'
import './caption.css'

// FollowPill offers the switch Alexa wanted to make while the user was busy
// with something else (typing, reading further up, in Reflect, or just
// after choosing the screen themselves): "Alexa is in the browser · Show",
// "New report · Open" (Appendix C.2). It floats at the foot of the
// exchange column.
export function FollowPill() {
  const pending = useFollow((f) => f.pending)
  const s = surfaceOf(pending)
  const title = useApp((a) => (s.kind === 'report' && s.id ? a.data.reports[s.id]?.title : undefined))
  if (!pending) return null
  return <FollowPillView key={pending} pending={pending} title={title} onShow={showPending} onDismiss={dismissPending} />
}

type ViewProps = { pending: string; title?: string; onShow: () => void; onDismiss: () => void }

// FollowPillView draws the pill for a surface key (browser, report:<id>,
// entry:<id>).
export function FollowPillView({ pending, title, onShow, onDismiss }: ViewProps) {
  const kind = surfaceOf(pending).kind
  const browser = kind === 'browser'
  const text = browser ? 'Alexa is in the browser' : kind === 'report' ? 'New report' : 'New details'
  return (
    <div className="follow-pill" role="status">
      <span className="fp-ic">{browser ? <Mark state="working" size="sm" /> : <Report size={16} />}</span>
      <span className="fp-text">
        {text}
        {title && <span className="fp-sub">{title}</span>}
      </span>
      <button type="button" className="fp-go" onClick={onShow}>
        {browser ? 'Show' : 'Open'}
      </button>
      <button type="button" className="icon-btn sm muted fp-x" aria-label="Not now" title="Not now" onClick={onDismiss}>
        <Close size={14} />
      </button>
    </div>
  )
}
