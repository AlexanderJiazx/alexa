import { ago } from '../../lib/format'
import { openExchange, openReport } from '../../state/actions'
import type { Done } from './home'
import { Section } from './Section'

// Recent is what Alexa finished lately (home.ts recentWork), as the app's
// divided rows: what it was and when, then a line of what came of it. A
// report opens beside the board; anything else opens Reflect at its
// exchange.
export function Recent({ items }: { items: Done[] }) {
  return (
    <Section title="Recent" className="home-recent">
      <ul className="list home-list">
        {items.map((d) => (
          <li key={d.key}>
            <button type="button" className="rowlink" onClick={() => ('report' in d ? openReport(d.report) : openExchange(d.exchange))}>
              <span className="hl-head">
                <span className="t1">{d.title}</span>
                <span className="hl-at num">{ago(d.at)}</span>
              </span>
              {d.line && <span className="t2">{d.line}</span>}
            </button>
          </li>
        ))}
      </ul>
    </Section>
  )
}
