import type { ReactNode } from 'react'

type Props = {
  icon: ReactNode
  title: string
  body?: ReactNode
  // action is usually one primary Button.
  action?: ReactNode
}

// EmptyState is what a list shows before it has anything: a glyph in a
// sunken circle, one line of headline, one of explanation, one action.
export function EmptyState({ icon, title, body, action }: Props) {
  return (
    <div className="empty-state">
      <span className="empty-glyph" aria-hidden="true">
        {icon}
      </span>
      <p className="empty-title">{title}</p>
      {body && <p className="empty-body">{body}</p>}
      {action && <div className="empty-action">{action}</div>}
    </div>
  )
}
