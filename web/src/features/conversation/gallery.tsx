import { Fragment } from 'react'
import type { Entry } from '../../api/types'
import { Section } from '../../dev/Section'
import type { GalleryPage } from '../../dev/types'
import { blankSession } from '../../state/reducer'
import { setApp, store } from '../../state/store'
import { HomeBoard } from '../home/HomeBoard'
import { StepsBlock } from '../progress/StepsBlock'
import { WorkingLine } from '../progress/WorkingLine'
import * as f from './fixtures'
import { Log } from './Log'
import { renderEntry, type EntryMode } from './registry'

// The conversation's gallery page: every entry kind, the steps, the working
// line and a whole exchange, laid out in the log's own column. Copy this
// file's shape for a feature's own page.

let seeded = false
// seed gives the page a signed-in user and a session at work, as the
// entries and the working line read them from the store.
function seed() {
  if (seeded) return
  seeded = true
  setApp({ user: { id: 1, username: 'alex', displayName: 'Alexander', email: '', timezone: 'Europe/London', about: '', isAdmin: false, createdAt: 0 }, boot: 'ready' })
  const d = store.get().data
  store.set((s) => ({ ...s, data: { ...d, loaded: true, session: { ...blankSession, status: 'working', activity: 'Comparing prices on amazon.com', browserUrl: 'https://www.amazon.com/' } } }))
}

function Col({ entries, mode = 'now' }: { entries: Entry[]; mode?: EntryMode }) {
  return (
    <div className="log-inner">
      {entries.map((e) => (
        <Fragment key={e.id}>{renderEntry(e, mode)}</Fragment>
      ))}
    </div>
  )
}

const page: GalleryPage = {
  name: 'conversation',
  title: 'Conversation',
  render() {
    seed()
    return (
      <>
        <Section title="Home: an empty exchange">
          <div className="log-inner">
            <HomeBoard />
          </div>
        </Section>
        <Section title="User">
          <Col entries={[f.user, f.userWithFile]} />
        </Section>
        <Section title="Say">
          <Col entries={[f.say, f.sayWithBlocks]} />
        </Section>
        <Section title="Ask, waiting">
          <Col entries={[f.askOpen, f.askFree]} />
        </Section>
        <Section title="Ask, answered">
          <Col entries={[f.askAnswered]} />
        </Section>
        <Section title="Take-over, waiting and done">
          <Col entries={[f.takeoverOpen, f.takeoverDone]} />
        </Section>
        <Section title="Login, waiting">
          <Col entries={[f.loginOpen]} />
        </Section>
        <Section title="Report">
          <Col entries={[f.report, f.reportUpdated]} />
        </Section>
        <Section title="Error and note">
          <Col entries={[f.error, f.note]} />
        </Section>
        <Section title="Steps, folded and live">
          <div className="log-inner">
            <StepsBlock steps={f.steps} live={false} />
            <StepsBlock steps={[...f.steps, f.runningStep]} live />
            <WorkingLine />
          </div>
        </Section>
        <Section title="Unknown kind and plan (not built yet)">
          <Col entries={[f.unknown, f.plan]} />
        </Section>
        <Section title="A whole exchange, in Reflect">
          <Log mode="history" entries={f.exchange} />
        </Section>
      </>
    )
  },
}

export default page
