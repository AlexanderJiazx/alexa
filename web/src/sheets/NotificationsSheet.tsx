import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { AppNotification } from '../api/types'
import { ago } from '../lib/format'
import { openRef } from '../state/actions'
import { useNotes } from '../state/selectors'
import { cx } from '../ui/cx'
import { EmptyState } from '../ui/EmptyState'
import { Bell } from '../ui/icons'
import { Group, Section } from './parts'
import type { SheetProps } from './registry'
import { Sheet } from './Sheet'

// keepNew is the notifications to show as new while the inbox is open:
// those that were new when it opened, and any new one that arrives. Opening
// marks them read on the server, whose changed:notifications brings the
// list back all read a moment later; they stay new here until the inbox
// closes (FH-10). It returns seen itself when nothing is added.
export function keepNew(seen: ReadonlySet<number>, items: AppNotification[]): ReadonlySet<number> {
  const add = items.filter((n) => !n.read && !seen.has(n.id))
  return add.length ? new Set([...seen, ...add.map((n) => n.id)]) : seen
}

// NotificationsSheet is the inbox: what Alexa told the user while they were
// away. Opening it marks them read; one about something opens it.
export function NotificationsSheet({ open, onClose }: SheetProps) {
  const items = useNotes()
  const [seen, setSeen] = useState<ReadonlySet<number>>(() => keepNew(new Set(), items))
  const fresh = keepNew(seen, items)
  if (fresh !== seen) setSeen(fresh)
  useEffect(() => {
    if (items.some((n) => !n.read)) api.markRead().catch(() => {})
  }, [items])
  const isNew = (n: AppNotification) => !n.read || fresh.has(n.id)
  const unread = items.filter(isNew).length
  return (
    <Sheet open={open} title="Notifications" onClose={onClose}>
      {items.length === 0 ? (
        <EmptyState icon={<Bell size={28} />} title="You’re all caught up" body="When Alexa finishes something or needs you while you’re away, it lands here, and on this device if you turn on notifications in Settings." />
      ) : (
        <Section title={unread ? `${unread} new` : undefined}>
          <Group>
            <ul className="row-list">
              {items.map((n) => (
                <li key={n.id} className={cx('row notice', isNew(n) && 'unread')}>
                  {/* The dot's gutter is kept only while some are new, so the titles line up. */}
                  {unread > 0 && <span className="row-lead">{isNew(n) && <span className="unread-dot" aria-label="New" />}</span>}
                  {/* Only a note about something opens it; one about nothing
                      in particular is just read, not a button that goes
                      nowhere. */}
                  {n.ref ? (
                    <button type="button" className="row-text rowlink" onClick={() => openRef(n.ref)}>
                      <NoteText n={n} />
                    </button>
                  ) : (
                    <div className="row-text">
                      <NoteText n={n} />
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </Group>
        </Section>
      )}
    </Sheet>
  )
}

function NoteText({ n }: { n: AppNotification }) {
  return (
    <>
      <span className="row-label">{n.title}</span>
      {n.body && <span className="row-sub clamp">{n.body}</span>}
      <span className="row-meta num">{ago(n.createdAt)}</span>
    </>
  )
}
