import { useEffect, useMemo, useState, type Ref } from 'react'
import type { Entry, Needs } from '../../api/types'
import { useBackgroundJob } from '../../state/exchange'
import { useConnection, useFeed, useSession, useUser } from '../../state/selectors'
import { cx } from '../../ui/cx'
import { Mark, type Presence } from '../../ui/Mark'
import { greeting, nextRun, recentWork, status, type Done } from './home'
import './home.css'
import { NeedsCard } from './NeedsCard'
import { Recent } from './Recent'
import { useHomeData } from './useHomeData'

// The clock the board reads: once a minute is plenty for "in 20 min".
function useMinute() {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 60e3)
    return () => clearInterval(t)
  }, [])
  return now
}

type BoardProps = {
  // centred puts the composer in the board, under the greeting (wide
  // screens with nothing beside the board); slot is where it goes.
  centred?: boolean
  slot?: Ref<HTMLDivElement>
  leaving?: boolean
}

// HomeBoard is what the main screen shows when there's no current exchange
// (WP-08): the dot, a greeting, a status line when there's something real
// to say, the composer, then what Alexa needs from you and what she
// finished lately. Each part is left out when it's empty.
export function HomeBoard({ centred, slot, leaving }: BoardProps) {
  const user = useUser()
  const session = useSession((s) => ({ status: s.status, needs: s.needs, control: s.control }))
  const feed = useFeed()
  const job = useBackgroundJob()
  const offline = useConnection() === 'offline'
  const data = useHomeData()
  const now = useMinute()

  const needs = session.status === 'needs' ? (session.needs ?? null) : null
  const entry = needs?.entryId ? feed.find((e) => e.id === needs.entryId) : undefined
  const busy = session.status !== 'idle'
  const recent = useMemo(() => recentWork(feed, data.reports, { now, busy }), [feed, data.reports, now, busy])

  return (
    <HomeView
      centred={centred}
      slot={slot}
      leaving={leaving}
      presence={needs ? 'needs' : job ? 'working' : 'idle'}
      greeting={greeting(user?.displayName || user?.username || '', new Date(now).getHours())}
      status={status({ offline, job, next: data.schedules ? nextRun(data.schedules, now) : undefined, now })}
      needs={needs}
      needsEntry={entry}
      control={session.control}
      recent={recent}
    />
  )
}

export type HomeViewProps = BoardProps & {
  presence: Presence
  greeting: string
  status: string | null
  needs: Needs | null
  needsEntry?: Entry
  control: boolean
  recent: Done[]
}

// HomeView draws the board from what it's given, so the gallery can show it
// in any state.
export function HomeView(p: HomeViewProps) {
  return (
    <div className={cx('home', p.centred && 'centred', p.leaving && 'leaving')}>
      <div className="home-in">
        <header className="home-top">
          <Mark state={p.presence} size="lg" draw />
          <h1 className="greet">{p.greeting}</h1>
          <p className="home-status">{p.status}</p>
        </header>
        {p.centred && <div ref={p.slot} className="home-slot" aria-hidden="true" />}
        <div className="home-more">
          {p.needs && <NeedsCard needs={p.needs} entry={p.needsEntry} control={p.control} />}
          {p.recent.length > 0 && <Recent items={p.recent} />}
        </div>
      </div>
    </div>
  )
}
