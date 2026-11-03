import { Fragment, useState } from 'react'
import type { Source } from '../../api/blocks'
import { cx } from '../../ui/cx'
import { Favicon, hostName } from '../../ui/Favicon'
import { Chevron, ToolGlyph } from '../../ui/icons'
import { Pill } from '../../ui/Pill'
import { StepStatusGlyph } from './glyphs'
import { spanText, type Group, type Step } from './group'

// FaviconStack is a few sites' icons, overlapping: where a run went.
export function FaviconStack({ hosts, size = 16, max = 3 }: { hosts: string[]; size?: number; max?: number }) {
  if (!hosts.length) return null
  return (
    <span className="favicons pg-favs" aria-hidden="true">
      {hosts.slice(0, max).map((h) => (
        <Favicon key={h} host={h} size={size} />
      ))}
    </span>
  )
}

// GroupIcon is a group's lead: the site's favicon for work on a site, else
// the glyph for its kind.
function GroupIcon({ g }: { g: Group }) {
  if (g.status === 'failed' || g.status === 'skipped' || g.status === 'interrupted') return <StepStatusGlyph status={g.status} />
  if (g.cat === 'web' && g.host) return <Favicon host={g.host} size={16} />
  return <ToolGlyph kind={g.cat === 'files' ? 'file' : g.kind} size={16} />
}

// GroupWords is a group's line in words: the site in a stronger weight,
// what was done there, and the counts after a dot.
function GroupWords({ g }: { g: Group }) {
  const site = g.cat === 'web' && g.items.length > 0
  const sourceHosts = unique(g.sources.map((s) => hostName(s.url)))
  return (
    <span className="pg-words">
      {g.head && <span className={site ? 'pg-site' : undefined}>{g.head}</span>}
      {g.items.length > 0 && (
        <span>
          {g.head ? ': ' : ''}
          {g.items.join(', ')}
        </span>
      )}
      {g.more > 0 && <span className="pg-more">, +{g.more} more</span>}
      {g.meta.map((m, i) => (
        <span key={m} className="pg-meta">
          <Sep />
          {g.cat === 'search' && i === g.meta.length - 1 && g.sources.length > 0 && <FaviconStack hosts={sourceHosts} size={14} />}
          {m}
        </span>
      ))}
    </span>
  )
}

const unique = (xs: string[]) => [...new Set(xs)]

// Sep is the dot between parts of a line. A bare " · " sits too tight in
// the UI face, so it gets a little room either side; the spaces stay for
// screen readers and copying.
export const Sep = () => <span className="pg-sep"> · </span>

// Chev is a line's disclosure chevron. A line that doesn't open keeps the
// chevron's place empty, so every line's time ends on one right edge.
export function Chev({ open, on = true }: { open: boolean; on?: boolean }) {
  return (
    <span className={cx('pg-chev', open && 'up', !on && 'none')} aria-hidden="true">
      {on && <Chevron size={14} />}
    </span>
  )
}

// Dotted draws a line whose parts are joined with " · " using Sep.
export function Dotted({ text }: { text: string }) {
  const parts = text.split(' · ')
  return (
    <>
      {parts.map((p, i) => (
        <Fragment key={i}>
          {i > 0 && <Sep />}
          {p}
        </Fragment>
      ))}
    </>
  )
}

type GroupProps = {
  g: Group
  // tight is the RunCard's nested form: no time column.
  tight?: boolean
}

// StepGroup is one line of a run's steps ("amazon.com: Opened the listing,
// Checked the price, +3 more"), which opens to every step in it. Its failed
// steps stay in view, with their errors, when it's closed.
export function StepGroup({ g, tight }: GroupProps) {
  const [open, setOpen] = useState(false)
  if (g.cat === 'chip') return <StepChips g={g} />
  const more = g.steps.length > 1 || g.sources.length > 0
  const time = tight || g.status === 'running' ? '' : spanText(g.start, g.end)
  const body = (
    <>
      <span className="pg-ic">
        <GroupIcon g={g} />
      </span>
      <span className="pg-text">
        <GroupWords g={g} />
      </span>
      {time && <span className="pg-time num">{time}</span>}
      <Chev open={open} on={more} />
    </>
  )
  const failedOnly = g.failed.length > 0 && g.failed.length < g.steps.length
  return (
    <div className={cx('pg-group', g.status === 'running' && 'running', tight && 'tight')}>
      {more ? (
        <button type="button" className="pg-line" aria-expanded={open} onClick={() => setOpen(!open)} title={g.text}>
          {body}
        </button>
      ) : (
        <div className="pg-line" title={g.text}>
          {body}
        </div>
      )}
      {open ? <StepList steps={g.steps} sources={g.cat === 'search' ? g.sources : []} /> : failedOnly && <FailedSteps steps={g.failed} />}
      {!open && !failedOnly && g.status === 'failed' && <FailedSteps steps={g.failed} errorsOnly />}
    </div>
  )
}

// StepList is every step of an opened group, each with its own glyph, and
// the pages a search found.
function StepList({ steps, sources }: { steps: Step[]; sources: Source[] }) {
  return (
    <div className="steps pg-list">
      <ol>
        {steps.map((s) => (
          <StepItem key={s.id} s={s} />
        ))}
      </ol>
      {sources.length > 0 && (
        <ul className="pg-sources" aria-label="Sources">
          {sources.map((src) => (
            <li key={src.url}>
              <a href={src.url} target="_blank" rel="noreferrer" title={src.title || src.url}>
                <Favicon host={src.url} size={14} />
                <span>{hostName(src.url)}</span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function StepItem({ s }: { s: Step }) {
  const off = s.status === 'failed' || s.status === 'skipped' || s.status === 'interrupted'
  const tag = s.status === 'skipped' ? 'Skipped' : s.status === 'interrupted' ? 'Stopped here' : ''
  return (
    <li className={cx('pg-item', s.status)}>
      <span className="pg-ic">{off ? <StepStatusGlyph status={s.status} /> : <ToolGlyph kind={s.kind} size={14} />}</span>
      <span className="pg-text">
        {s.label}
        {tag && (
          <span className="pg-tag">
            <Sep />
            {tag}
          </span>
        )}
        {s.error && <span className="pg-err">{s.error}</span>}
      </span>
    </li>
  )
}

// FailedSteps keeps a group's failed steps in view while it's closed.
export function FailedSteps({ steps, errorsOnly }: { steps: Step[]; errorsOnly?: boolean }) {
  if (errorsOnly) {
    const errs = unique(steps.map((s) => s.error ?? '').filter(Boolean))
    if (!errs.length) return null
    return (
      <div className="pg-fails errs">
        {errs.map((e) => (
          <span key={e} className="pg-err">
            {e}
          </span>
        ))}
      </div>
    )
  }
  return (
    <ol className="pg-fails">
      {steps.map((s) => (
        <StepItem key={s.id} s={s} />
      ))}
    </ol>
  )
}

// StepChips shows memory and schedule steps as chips: what Alexa now
// remembers, and what it set up for later.
function StepChips({ g }: { g: Group }) {
  return (
    <div className="pg-chips">
      {g.steps.map((s) => (
        <Pill key={s.id} className="pg-chip">
          <ToolGlyph kind={s.kind} size={14} />
          <span className="pg-chip-label">{s.label}</span>
        </Pill>
      ))}
    </div>
  )
}
