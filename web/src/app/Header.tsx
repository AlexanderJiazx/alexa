import { ConnectionPill } from '../composer/ConnectionPill'
import { runTitle } from '../features/palette/context'
import { hints } from '../features/palette/shortcuts'
import './layout.css'
import { home, openReflect, toggleMenu } from '../state/actions'
import { current, jobLabel, split } from '../state/exchange'
import { useNeeds, useSession, useUI, useUnread, useUser } from '../state/selectors'
import { useApp } from '../state/store'
import { cx } from '../ui/cx'
import { History } from '../ui/icons'
import { Mark, type Presence } from '../ui/Mark'

// Header is the bar across the top (52px, 48 on phones): Alexa's dot (home),
// what Alexa is on in the centre while it works, then Reflect and the
// account menu.
export function Header() {
  const user = useUser()
  const s = useSession((v) => ({ status: v.status, control: v.control }))
  // A string, so the header re-renders only when the title changes. A
  // scheduled job working off screen names itself, not the exchange on
  // screen (which isn't what Alexa is on).
  const title = useApp((st) => {
    const { session, feed } = st.data
    const job = session.status === 'working' ? split(feed).job : null
    return job ? `${jobLabel(job)} · scheduled` : runTitle(session, current(feed))
  })
  const needs = useNeeds()
  const unread = useUnread()
  const menu = useUI((u) => u.menu)
  const presence: Presence = s.control ? 'waiting' : needs ? 'needs' : s.status === 'working' ? 'working' : 'idle'
  return <HeaderBar presence={presence} title={title} name={user?.displayName || user?.username || ''} unread={unread} badge={unread > 0 || !!needs} menu={menu} />
}

const presenceLabel: Record<Presence, string> = { idle: 'Alexa', working: 'Alexa is working', waiting: 'You’re in control', needs: 'Alexa needs you' }

type BarProps = { presence: Presence; title: string; name: string; unread: number; badge: boolean; menu: boolean }

// HeaderBar is the header as drawn, from what it shows; the gallery draws
// it in each state.
export function HeaderBar({ presence, title, name, unread, badge, menu }: BarProps) {
  return (
    <header className="header">
      <button type="button" className="mark-btn" aria-label={`Home. ${presenceLabel[presence]}`} title="Home" onClick={home}>
        <Mark state={presence} />
      </button>
      <ConnectionPill />
      <p className={cx('header-context', title && 'on')}>{title}</p>
      <div className="end">
        <button type="button" className="icon-btn" aria-label="Reflect" title={`Reflect (${hints.reflect})`} onClick={() => openReflect()}>
          <History size={20} />
        </button>
        <button type="button" id="account-button" className="avatar-btn" aria-label={unread ? `Account menu, ${unread} unread` : 'Account menu'} aria-haspopup="menu" aria-expanded={menu} aria-controls={menu ? 'account-menu' : undefined} onClick={toggleMenu}>
          <span className="avatar">{name.slice(0, 1).toUpperCase()}</span>
          {badge && <span className="badge" />}
        </button>
      </div>
    </header>
  )
}
