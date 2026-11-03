import type { CSSProperties } from 'react'
import { cx } from '../../ui/cx'
import type { Action } from './action'
import type { Size } from './input'

// Box is the element Alexa is acting on, in page pixels. The server will
// send it with each pointer move (proposed: BrowserEvent.box); until then
// there's no ring, only the bubble.
export type Box = { x: number; y: number; w: number; h: number }

// ActionBubble says what Alexa is doing where its pointer is: the verb, the
// element's label, and what it's typing (dots for a secret). floating puts
// it at the foot of the page when there's no pointer to ride along with.
export function ActionBubble({ action, floating }: { action: Action; floating?: boolean }) {
  return (
    <span className={cx('bp-act', floating && 'floating')}>
      <span className="bp-act-line">
        <span className="bp-verb">{action.verb}</span>
        {action.label && <span className="bp-label">{action.page ? action.label : `“${action.label}”`}</span>}
      </span>
      {action.typing && (
        <span className={cx('bp-typed', action.typing.secret && 'secret')}>
          <span className="bp-typed-text">{action.typing.text}</span>
          <span className="bp-caret" />
        </span>
      )}
    </span>
  )
}

// TargetRing is a soft ring around the element being acted on.
export function TargetRing({ box, size }: { box: Box; size: Size }) {
  const style = {
    '--l': `${(box.x / size.w) * 100}%`,
    '--t': `${(box.y / size.h) * 100}%`,
    '--w': `${(box.w / size.w) * 100}%`,
    '--h': `${(box.h / size.h) * 100}%`,
  } as CSSProperties
  return <span className="bp-ring" style={style} aria-hidden="true" />
}
