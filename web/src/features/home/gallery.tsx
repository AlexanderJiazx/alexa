import { useEffect, useState } from 'react'
import type { Entry, Needs, SessionState, User } from '../../api/types'
import { Header } from '../../app/Header'
import { Stage } from '../../app/Stage'
import { prefillComposer } from '../../composer/focus'
import { Section } from '../../dev/Section'
import type { GalleryPage } from '../../dev/types'
import { clearExchange, followStart, followStore, restoreExchange } from '../../state/exchange'
import { blankSession } from '../../state/reducer'
import { dispatch, initialUI, setApp, store } from '../../state/store'
import { BackgroundChip } from '../caption/BackgroundChip'
import { FollowChipView } from '../caption/FollowChip'
import { FollowPillView } from '../caption/FollowPill'
import { Suggestions } from '../conversation/Suggestions'
import * as f from './fixtures'
import './gallery.css'
import { greeting, nextRun, recentWork, status } from './home'
import { HomeView, type HomeViewProps } from './HomeBoard'
import { installStubs, later, stopScript } from './stubs'

// The stage's gallery page (?gallery=stage): the whole main screen with
// fixtures (the home board, a finished exchange beside its report, Alexa in
// the browser, a switch held back as a pill, a background job), then the
// home board's states and the small pieces on their own. Nothing here
// reaches a server: /api and the live socket are stubbed, and a message
// sent from the composer gets a short scripted run back.
//
// &s=<scenario> on a frame's URL shows that scenario alone, full screen.

type Scenario = { name: string; title: string; user: User; feed: () => Entry[]; session?: Partial<SessionState>; home?: boolean; then?: () => void }

const now = Date.now()
const waiting = f.waiting()
const working = (activity: string): Partial<SessionState> => ({ status: 'working', activity, runStartedAt: now - 40e3 })

// A run in the browser, part of the way through.
function browsing(): Entry[] {
  const [user, ...steps] = f.finished().slice(0, 4)
  return [user, ...steps.slice(0, 2), { ...steps[2], status: 'running', doneText: undefined }]
}

const scenarios: Scenario[] = [
  { name: 'home', title: 'Home', user: f.regular, feed: f.past, home: true },
  { name: 'new', title: 'Someone new', user: f.newcomer, feed: () => [], home: true },
  {
    name: 'needs',
    title: 'A question waiting',
    user: f.regular,
    feed: () => waiting.feed,
    home: true,
    session: { status: 'needs', needs: { kind: 'question', message: waiting.ask.text, options: waiting.ask.data?.options, recommended: 0, entryId: waiting.ask.id } },
  },
  { name: 'done', title: 'Finished, with its report', user: f.regular, feed: f.finished, session: { surface: { kind: 'report', id: 'r1' } } },
  { name: 'browser', title: 'In the browser', user: f.regular, feed: browsing, session: { ...working('Checking delivery dates on bestbuy.com'), surface: { kind: 'browser' }, browserUrl: 'https://www.bestbuy.com/site/sony-wh1000xm6' } },
  {
    name: 'pill',
    title: 'Held back while typing',
    user: f.regular,
    feed: f.finished,
    session: { surface: { kind: 'report', id: 'r1' } },
    // The user starts typing; then a scheduled run opens the browser.
    then() {
      later(500, () => prefillComposer('Can you also check if it comes in white'))
      later(1800, () => {
        const d = store.get().data
        dispatch({ type: 'snapshot', snap: { state: { ...d.session, ...working('Morning briefing'), surface: { kind: 'browser' } }, feed: [...d.feed, ...f.job()], hasMore: false } })
      })
    },
  },
  { name: 'job', title: 'A job in the background', user: f.regular, feed: () => [...f.finished(), ...f.job()], session: working('Searching the web') },
  {
    name: 'jobpill',
    title: 'A job browses while you read',
    user: f.regular,
    feed: f.finished,
    session: { surface: { kind: 'report', id: 'r1' } },
    // A scheduled run starts and opens a page. The server leaves the screen
    // alone (R6); the pill offers to show the browser.
    then() {
      later(1200, () => {
        const d = store.get().data
        dispatch({ type: 'snapshot', snap: { state: { ...d.session, ...working('Reading bbc.com') }, feed: [...d.feed, ...f.jobBrowsing()], hasMore: false } })
      })
    },
  },
]

let current = ''

// seed puts a scenario in the store, the way a snapshot from the server
// would, and lets go of what the last one left behind.
function seed(s: Scenario) {
  stopScript()
  current = s.name
  const feed = s.feed()
  followStore.set(followStart)
  setApp({ user: s.user, boot: 'ready', connection: 'live', config: null })
  store.set((a) => ({ ...a, ui: initialUI }))
  dispatch({ type: 'clear' })
  if (s.name !== 'new') for (const r of f.reports) dispatch({ type: 'report', report: r })
  dispatch({ type: 'snapshot', snap: { state: { ...blankSession, ...s.session }, feed, hasMore: false } })
  if (s.home) clearExchange()
  else if (feed[0]) restoreExchange(feed[0])
}

const byName = (n: string | null) => scenarios.find((s) => s.name === n) ?? scenarios[0]

// Demo is the app with one scenario in it: the header, the stage and its
// composer. A scenario starts on a fresh stage, seeded from a timer as a
// snapshot from the server would be, so the stage follows it as Alexa's.
function Demo({ full }: { full?: boolean }) {
  const [name, setName] = useState(() => byName(new URLSearchParams(location.search).get('s')).name)
  const [ready, setReady] = useState(false)
  const [round, setRound] = useState(0)
  const pick = (n: string) => {
    setReady(false)
    setName(n)
    setRound((r) => r + 1)
  }
  useEffect(() => {
    const t = setTimeout(() => {
      const s = byName(name)
      seed(s)
      setReady(true)
      s.then?.()
    }, 0)
    return () => clearTimeout(t)
  }, [name, round])
  return (
    <div className={full ? 'g-full' : 'g-stage'}>
      {!full && (
        <div className="seg g-scenes" role="radiogroup" aria-label="Scenario">
          {scenarios.map((s) => (
            <button key={s.name} type="button" role="radio" aria-checked={s.name === name} onClick={() => pick(s.name)}>
              {s.title}
            </button>
          ))}
        </div>
      )}
      <div className={full ? 'app' : 'app g-app'}>
        {ready && (
          <>
            <Header />
            <Stage />
          </>
        )}
      </div>
    </div>
  )
}

// ---------- the board on its own ----------

const hour = new Date(now).getHours()
const noop = () => {}
const takeover: Needs = { kind: 'takeover', message: 'Best Buy wants a code it sent to your phone. Can you type it in?', site: 'bestbuy.com' }

const recent = recentWork(f.past(), f.reports, { now, busy: false })

const board = (p: Partial<HomeViewProps>): HomeViewProps => ({
  presence: 'idle',
  greeting: greeting('Alexander', hour),
  status: null,
  needs: null,
  control: false,
  recent: [],
  ...p,
})

function Board(p: Partial<HomeViewProps>) {
  return (
    <div className="g-board">
      <HomeView {...board(p)} />
    </div>
  )
}

function Chip() {
  const [on, setOn] = useState(true)
  return <FollowChipView on={on} onChange={setOn} />
}

function Ups() {
  const [round, setRound] = useState(0)
  return (
    <div className="g-col">
      <Suggestions
        key={round}
        suggestions={['Order the Sony', 'Compare battery life', 'Watch the price for a week']}
        onPick={() => {
          setTimeout(() => setRound((r) => r + 1), 1200)
        }}
      />
    </div>
  )
}

const page: GalleryPage = {
  name: 'stage',
  title: 'Stage and home',
  render() {
    installStubs({ fresh: () => current === 'new' })
    if (new URLSearchParams(location.search).has('s')) return <Demo full />
    return (
      <>
        <Section title="The main screen (send a message to see a run)">
          <Demo />
        </Section>
        <Section title="Home: coming back, a run due within the day">
          <Board status={status({ offline: false, next: nextRun(f.schedules, now), now })} recent={recent} />
        </Section>
        <Section title="Home: nothing to say, nothing finished lately">
          <Board />
        </Section>
        <Section title="Home: Alexa needs you, a job in the background">
          <Board presence="needs" status={status({ offline: false, job: 'Morning briefing', now })} needs={takeover} recent={recent} />
        </Section>
        <Section title="Home: offline">
          <Board status={status({ offline: true, now })} recent={recent.slice(0, 2)} />
        </Section>
        <Section title="A switch held back">
          <div className="g-pills">
            <FollowPillView pending="browser" onShow={noop} onDismiss={noop} />
            <FollowPillView pending="report:r1" title="Noise-cancelling headphones under $300" onShow={noop} onDismiss={noop} />
            <FollowPillView pending="entry:5001" onShow={noop} onDismiss={noop} />
          </div>
        </Section>
        <Section title="Following, in a panel’s head">
          <div className="g-pills">
            <Chip />
            <FollowChipView on={false} onChange={noop} />
          </div>
        </Section>
        <Section title="A job in the background">
          <BackgroundChip label="Morning briefing" />
        </Section>
        <Section title="Follow-ups under the final say">
          <Ups />
        </Section>
      </>
    )
  },
}

export default page
