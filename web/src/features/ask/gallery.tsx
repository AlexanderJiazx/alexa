import { Fragment } from 'react'
import type { Entry } from '../../api/types'
import { Composer } from '../../composer/Composer'
import { ConnectionPillView } from '../../composer/ConnectionPill'
import { setTransport, type Item } from '../../composer/outbox'
import { receiptFor, ReceiptLine, type ReceiptProps } from '../../composer/Receipts'
import { Section } from '../../dev/Section'
import type { GalleryPage } from '../../dev/types'
import { blankSession } from '../../state/reducer'
import { setApp, store } from '../../state/store'
import { renderEntry, type EntryMode } from '../conversation/registry'
import { answersIn } from './answer'
import { NeedsDock } from './NeedsDock'
import { StoppedCard } from './StoppedCard'
import * as f from './fixtures'
import './gallery.css'

// The ask's gallery page (?gallery=ask): every needs-you card, the
// failures, the dock above the composer, the composer itself and each of
// its receipts. The composer here sends nowhere: a message with "fail" in
// it can't go, so Retry can be tried; anything else goes after a moment.

let seeded = false
function seed() {
  if (seeded) return
  seeded = true
  setApp({ user: { id: 1, username: 'alex', displayName: 'Alexander', email: '', timezone: 'Europe/London', about: '', isAdmin: false, createdAt: 0 }, boot: 'ready' })
  const d = store.get().data
  store.set((s) => ({ ...s, data: { ...d, loaded: true, session: { ...blankSession, status: 'needs', needs: f.needsChoice } } }))
  setTransport(
    (m) =>
      new Promise((ok, no) =>
        setTimeout(() => {
          if (/fail/i.test(m.text)) no(new TypeError('Failed to fetch'))
          else ok(null)
        }, 900),
      ),
  )
}

// Col lays entries out as the log does: an answered ask knows its answer,
// and a reply that only repeats the choice isn't shown again.
function Col({ entries, mode = 'now' }: { entries: Entry[]; mode?: EntryMode }) {
  const answers = answersIn(entries)
  return (
    <div className="log-inner ask-g-col">
      {entries
        .filter((e) => !answers.repeats.has(e.id))
        .map((e) => (
          <Fragment key={e.id}>{renderEntry(e, mode, undefined, answers.of.get(e.id))}</Fragment>
        ))}
    </div>
  )
}

// The receipts, one of each, as the composer would show them.
const quiet: ReceiptProps = { working: false, typing: false, onInterrupt: () => {}, onRetried: () => {} }
const item = (state: Item['state'], text: string, more: Partial<Item> = {}): Item => ({ clientId: `g-${state}`, msg: { text }, from: 'composer', state, at: Date.now(), ...more })
const receipts = [
  receiptFor([item('failed', 'Book the 7:30 table', { error: 'You’re offline.' })], quiet, false),
  receiptFor([], { ...quiet, working: true, typing: true }, false),
  receiptFor([], quiet, true),
  receiptFor([item('sending', 'And a high chair', { slow: true })], quiet, false),
  receiptFor([item('queued', 'Make it 8 instead', { entryId: 1 })], quiet, false),
  receiptFor([item('queued', 'Do this now', { entryId: 1, msg: { text: 'Do this now', interrupt: true } })], quiet, false),
  receiptFor([item('seen', 'Make it 8 instead', { entryId: 1 })], quiet, false),
]

const page: GalleryPage = {
  name: 'ask',
  title: 'Ask and composer',
  render() {
    seed()
    return (
      <>
        <Section title="Choice: Alexa recommends the second">
          <Col entries={[f.choice]} />
        </Section>
        <Section title="Choice: no recommendation">
          <Col entries={[f.choiceEven]} />
        </Section>
        <Section title="Choice: long options read as rows">
          <Col entries={[f.choiceRows]} />
        </Section>
        <Section title="Pick any">
          <Col entries={[f.multi]} />
        </Section>
        <Section title="Small form">
          <Col entries={[f.form]} />
        </Section>
        <Section title="Form with a private-looking field left out">
          <Col entries={[f.formSecret]} />
        </Section>
        <Section title="Confirm a purchase">
          <Col entries={[f.confirm]} />
        </Section>
        <Section title="Confirm, no receipt">
          <Col entries={[f.confirmSend]} />
        </Section>
        <Section title="Free answer">
          <Col entries={[f.free]} />
        </Section>
        <Section title="Answered, and in Reflect">
          <Col entries={[f.answered, f.answeredReply, f.answeredMulti, f.answeredMultiReply, f.answeredForm, f.answeredFormReply, f.unanswered]} />
          <Col entries={[f.confirm]} mode="history" />
        </Section>
        <Section title="Take-over and sign-in">
          <Col entries={[f.takeover, f.login]} />
        </Section>
        <Section title="Failures">
          <Col entries={[f.failRetry]} />
          <Col entries={[f.failWait]} />
          <Col entries={[f.failInput]} />
          <Col entries={[f.failLoop]} />
          <Col entries={[f.failOld]} />
        </Section>
        <Section title="Stopped">
          <div className="log-inner ask-g-col">
            <StoppedCard entry={f.stopped} live />
          </div>
        </Section>
        <Section title="Needs you, docked above the composer">
          <div className="app ask-g-app">
            <p className="ask-g-ctx">The question’s card is scrolled away, so the dock carries it. Try a message with “fail” in it to see Retry.</p>
            <NeedsDock />
            <Composer />
          </div>
        </Section>
        <Section title="Receipts">
          <div className="ask-g-receipts">
            {receipts.map((r, i) =>
              r ? (
                <div key={i} className="ask-g-receipt">
                  <ReceiptLine row={r} />
                </div>
              ) : null,
            )}
          </div>
        </Section>
        <Section title="Connection">
          <div className="ask-g-pills">
            <ConnectionPillView offline={false} />
            <ConnectionPillView offline />
          </div>
        </Section>
      </>
    )
  },
}

export default page
