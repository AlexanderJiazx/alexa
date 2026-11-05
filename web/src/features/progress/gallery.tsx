import type { ReactNode } from 'react'
import '../../app/layout.css'
import type { Entry } from '../../api/types'
import { Section } from '../../dev/Section'
import type { GalleryPage } from '../../dev/types'
import { blankSession, type RunInfo } from '../../state/reducer'
import { setApp, store } from '../../state/store'
import { Log } from '../conversation/Log'
import { renderEntry } from '../conversation/registry'
import * as f from './fixtures'
import './gallery.css'
import { FailedGlyph, InterruptedGlyph, OutcomeGlyph, Pie, PlanGlyph, SkippedGlyph } from './glyphs'
import { toSteps } from './group'
import { workingLine } from './phase'
import { RunCard, RunCardView } from './RunCard'
import { blockContext } from './runState'
import { StepsView } from './StepsBlock'
import { WorkingLineView } from './WorkingLine'

// The progress page (?gallery=progress): the RunCard, the working line in
// every phase, steps live and folded, the three ways a run ends, and the
// main screen at work, all from fixtures.

let seeded = false
// seed puts the fixture conversation in the store with Alexa at work on the
// live exchange's plan, as the RunCard, the Log and the working line read it.
function seed() {
  if (seeded) return
  seeded = true
  setApp({ user: { id: 1, username: 'alex', displayName: 'Alexander', email: '', timezone: 'Europe/London', about: '', isAdmin: false, createdAt: 0 }, boot: 'ready' })
  const d = store.get().data
  const session = { ...blankSession, status: 'working' as const, activity: 'Checking delivery dates', browserUrl: 'https://www.bestbuy.com/', phase: 'tool' as const, runStartedAt: f.t0, plan: f.planLive }
  store.set((s) => ({ ...s, data: { ...d, loaded: true, feed: f.feed, session, run: f.runs.tool } }))
}

const steps14 = toSteps(f.run14)

// ctxOf reads how a fixture run ended, as the Log's steps do from the store.
function ctxOf(run: Entry[]) {
  const steps = run.filter((e) => e.kind === 'step')
  return blockContext(f.feed, steps[0].id, steps[steps.length - 1].id, 'idle', null)
}
const stepsOf = (run: Entry[]) => run.filter((e) => e.kind === 'step')

function Col({ children }: { children: ReactNode }) {
  return <div className="log-inner pg-col">{children}</div>
}

function Line({ run, plan, activity, control }: { run: RunInfo | null; plan?: string; activity?: string; control?: boolean }) {
  return <WorkingLineView line={workingLine({ activity: activity ?? '', control: !!control }, run, plan)} since={f.t0} control={control} onStop={() => {}} appearAfter={0} />
}

const page: GalleryPage = {
  name: 'progress',
  title: 'Progress',
  render() {
    seed()
    return (
      <>
        <Section title="The main screen at work on a plan">
          <div className="pg-demo">
            <Log mode="now" entries={f.liveExchange} head={<RunCard />} />
          </div>
        </Section>
        <Section title="RunCard: 2 of 4, the active step at work">
          <Col>
            <RunCardView plan={f.planLive} steps={steps14} onStop={() => {}} />
          </Col>
        </Section>
        <Section title="RunCard: a plan that changed course, and one waiting on you">
          <Col>
            <RunCardView plan={f.planMixed} steps={[]} onStop={() => {}} />
            <RunCardView plan={f.planLive} steps={steps14} needs />
          </Col>
        </Section>
        <Section title="Working line: thinking, on a tool, searching">
          <Col>
            <Line run={f.runs.thinking} plan="Check delivery by Friday" />
            <Line run={f.runs.tool} plan="Check delivery by Friday" />
            <Line run={f.runs.tool} />
            <Line run={f.runs.search} />
            <Line run={null} activity="Comparing prices on bestbuy.com" />
          </Col>
        </Section>
        <Section title="Working line: retrying, tidying up, waiting, in control, stopping">
          <Col>
            <Line run={f.runs.retrying} />
            <Line run={f.runs.compacting} />
            <Line run={f.runs.waiting} />
            <Line run={f.runs.tool} control />
            <Line run={f.runs.stopping} />
          </Col>
        </Section>
        <Section title="Writing: a reply, a question, a report">
          <Col>
            <Line run={f.runs.writingSay} />
            <Line run={f.runs.writingAsk} />
            <Line run={f.runs.writingReport} />
          </Col>
        </Section>
        <Section title="Steps while working: grouped, the older ones folded">
          <Col>
            <StepsView steps={f.longRun} live ctx={{ outcome: 'open' }} />
            <Line run={f.runs.tool} plan="Check delivery by Friday" />
          </Col>
        </Section>
        <Section title="Steps from a server with no step data">
          <Col>
            <StepsView steps={f.plainRun} live ctx={{ outcome: 'open' }} />
            <Line run={null} activity="Checking delivery dates" />
          </Col>
        </Section>
        <Section title="Finished: done, stopped, failed">
          <Col>
            <StepsView steps={stepsOf(f.doneRun)} live={false} ctx={ctxOf(f.doneRun)} />
            <StepsView steps={stepsOf(f.stoppedRun)} live={false} ctx={ctxOf(f.stoppedRun)} />
            <StepsView steps={stepsOf(f.failedRun)} live={false} ctx={ctxOf(f.failedRun)} />
            <StepsView steps={f.plainRun} live={false} ctx={{ outcome: 'done', ms: 95e3 }} />
          </Col>
        </Section>
        <Section title="Memory and schedule steps are chips">
          <Col>
            <StepsView steps={f.chipSteps} live ctx={{ outcome: 'open' }} />
          </Col>
        </Section>
        <Section title="The plan afterwards, and in Reflect">
          <Col>
            {renderEntry(f.planDoneEntry, 'history')}
            {renderEntry(f.planMixedEntry, 'history')}
          </Col>
        </Section>
        <Section title="Glyphs: to do, under way, done, failed, skipped, interrupted; outcomes; the plan’s pie">
          <div className="pg-legend">
            <PlanGlyph status="pending" />
            <PlanGlyph status="active" />
            <PlanGlyph status="done" />
            <FailedGlyph />
            <SkippedGlyph />
            <InterruptedGlyph />
            <span className="pg-legend-gap" />
            <OutcomeGlyph outcome="done" />
            <OutcomeGlyph outcome="stopped" />
            <OutcomeGlyph outcome="failed" />
            <OutcomeGlyph outcome="open" />
            <span className="pg-legend-gap" />
            <Pie done={0} total={4} />
            <Pie done={1} total={4} />
            <Pie done={2} total={4} />
            <Pie done={3} total={4} />
            <Pie done={4} total={4} />
          </div>
        </Section>
        <Section title="Reflect: the whole conversation">
          <Log mode="history" entries={f.feed.filter((e) => !f.liveExchange.includes(e))} />
        </Section>
      </>
    )
  },
}

export default page
