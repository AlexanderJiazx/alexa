import { Fragment, useState } from 'react'
import '../../app/layout.css'
import type { Entry, Report } from '../../api/types'
import { Section } from '../../dev/Section'
import type { GalleryPage } from '../../dev/types'
import { renderEntry } from '../../features/conversation/registry'
import { blankSession } from '../../state/reducer'
import { setApp, store } from '../../state/store'
import * as f from './fixtures'
import './gallery.css'
import { ReportLoading, ReportView } from './ReportPanel'

// The report's gallery page: its cards in the exchange, each fixture as a
// document in the panel, an HTML page on the theme bridge, and loading.
// Nothing here talks to the server: closing and alexa.send are stubbed.
//
// Two options in the address, for looking at one thing closely:
// - only=<cards|hn|trip|tables|blocks|page|loading> shows just that section;
// - full lets each panel grow to its whole document instead of scrolling.

let seeded = false
// seed puts the fixtures in the store, with the Hacker News report open,
// as the cards read them from there.
function seed() {
  if (seeded) return
  seeded = true
  setApp({ user: { id: 1, username: 'alex', displayName: 'Alexander', email: '', timezone: 'Europe/Lisbon', about: '', isAdmin: false, createdAt: 0 }, boot: 'ready' })
  const d = store.get().data
  const reports = Object.fromEntries(f.reports.map((r) => [r.id, r]))
  store.set((s) => ({ ...s, data: { ...d, loaded: true, reports, session: { ...blankSession, surface: { kind: 'report', id: f.hn.id } } } }))
}

const noop = () => {}

function Cards({ entries }: { entries: Entry[] }) {
  return (
    <div className="log-inner g-rep-cards">
      {entries.map((e) => (
        <Fragment key={e.id}>{renderEntry(e, 'now')}</Fragment>
      ))}
    </div>
  )
}

const q = () => new URLSearchParams(location.search)
const stage = () => (q().has('full') ? 'g-rep-stage full' : 'g-rep-stage')

function Doc({ report }: { report: Report }) {
  return (
    <div className={stage()}>
      <ReportView report={report} onClose={noop} onSend={noop} />
    </div>
  )
}

// Page shows the HTML report with alexa.send going to a list here instead
// of to Alexa.
function Page() {
  const [sent, setSent] = useState<string[]>([])
  return (
    <>
      <div className={stage()}>
        <ReportView report={f.page} onClose={noop} onSend={(t) => setSent((s) => [...s, t])} />
      </div>
      <ul className="g-rep-sent" aria-label="Sent from the report">
        {sent.length ? sent.map((t, i) => <li key={i}>{t}</li>) : <li className="none">Nothing sent yet. Tap a button in the page.</li>}
      </ul>
    </>
  )
}

const page: GalleryPage = {
  name: 'report',
  title: 'Report',
  render() {
    seed()
    const only = q().get('only')
    const show = (name: string) => !only || only === name
    return (
      <>
        {show('cards') && (
          <Section title="Cards in the exchange (the first is open)">
            <Cards entries={[f.cardHn, f.cardTrip, f.cardThumbs, f.cardBlocks, f.cardTables, f.cardPage, f.cardBare]} />
          </Section>
        )}
        {show('hn') && (
          <Section title="Hacker News, as a real run wrote it">
            <Doc report={f.hn} />
          </Section>
        )}
        {show('trip') && (
          <Section title="Key figures, prose between blocks, dates, files and sources">
            <Doc report={f.trip} />
          </Section>
        )}
        {show('tables') && (
          <Section title="Tables, lists and code">
            <Doc report={f.tables} />
          </Section>
        )}
        {show('blocks') && (
          <Section title="Blocks only, with the takeaway as its lede">
            <Doc report={f.blocksOnly} />
          </Section>
        )}
        {show('page') && (
          <Section title="An HTML page on the theme bridge">
            <Page />
          </Section>
        )}
        {show('loading') && (
          <Section title="Loading, and a report that’s gone">
            <div className="g-rep-pair">
              <div className="g-rep-stage short">
                <ReportLoading onClose={noop} />
              </div>
              <div className="g-rep-stage short">
                <ReportLoading failed onRetry={noop} onClose={noop} />
              </div>
            </div>
          </Section>
        )}
      </>
    )
  },
}

export default page
