import { api } from '../../api/client'
import type { CalEvent, Link, ReportItem } from '../../api/types'
import { useUser } from '../../state/selectors'
import { Favicon, hostName } from '../../ui/Favicon'
import { Download, File } from '../../ui/icons'
import { isImage } from './html'
import { Away } from './icons'
import { eventWhen } from './text'

// The parts of a report that come as data rather than prose: its key rows,
// dates, files and sources.

// A detail short enough to read as a figure in a tile ("$412", "2 nights").
const figure = (d?: string) => !!d && d.length <= 14

// Items are the report's key rows: up to four short figures read as a row
// of stats; anything else as label and detail rows.
export function Items({ items }: { items: ReportItem[] }) {
  if (!items.length) return null
  if (items.length >= 2 && items.length <= 4 && items.every((it) => figure(it.detail)))
    return (
      <dl className="rep-stats" data-n={items.length}>
        {items.map((it, i) => (
          <div key={i} className="rep-stat">
            <dt>{it.label}</dt>
            <dd>{it.detail}</dd>
          </div>
        ))}
      </dl>
    )
  return (
    <dl className="rep-keys">
      {items.map((it, i) => (
        <div key={i} className="rep-key">
          <dt>{it.label}</dt>
          {it.detail && <dd>{it.detail}</dd>}
        </div>
      ))}
    </dl>
  )
}

// Events are dates the user may want in their calendar, each with a date
// tile.
export function Events({ events }: { events: CalEvent[] }) {
  const tz = useUser()?.timezone
  if (!events.length) return null
  return (
    <section className="rep-sec" aria-labelledby="rep-dates">
      <h2 id="rep-dates" className="rep-sec-h">
        Dates
      </h2>
      <ul className="rep-events">
        {events.map((ev, i) => {
          const w = eventWhen(ev, tz)
          return (
            <li key={i} className="rep-event">
              <span className="rep-date" aria-hidden="true">
                <span className="m">{w?.month ?? '—'}</span>
                <span className="d">{w?.day ?? ''}</span>
              </span>
              <span className="rep-ev-t">
                <b>{ev.title}</b>
                <span>{[w?.line, ev.location].filter(Boolean).join(' · ')}</span>
              </span>
            </li>
          )
        })}
      </ul>
    </section>
  )
}

const ext = (f: string) => f.split('.').pop()?.toUpperCase() ?? ''
const base = (f: string) => f.split('/').pop() ?? f

// Files are what the report brought back: images inline, the rest as rows
// to download.
export function Files({ files }: { files: string[] }) {
  const images = files.filter(isImage)
  const others = files.filter((f) => !isImage(f))
  if (!files.length) return null
  return (
    <>
      {images.map((f) => (
        <figure key={f} className="rep-figure">
          <a href={api.fileURL(f, true)} target="_blank" rel="noopener noreferrer">
            <img src={api.fileURL(f, true)} alt={base(f)} loading="lazy" decoding="async" />
          </a>
          <figcaption>{base(f)}</figcaption>
        </figure>
      ))}
      {others.length > 0 && (
        <section className="rep-sec" aria-labelledby="rep-files">
          <h2 id="rep-files" className="rep-sec-h">
            Files
          </h2>
          <ul className="rep-files">
            {others.map((f) => (
              <li key={f}>
                <a className="rep-file" href={api.fileURL(f)} download>
                  <span className="rep-file-ic">
                    <File size={18} />
                  </span>
                  <span className="rep-file-t">
                    <b>{base(f)}</b>
                    <span>{ext(f)}</span>
                  </span>
                  <Download size={18} />
                </a>
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  )
}

// Sources close the report: numbered rows with the site's icon, the page's
// title and its address.
export function Sources({ sources }: { sources: Link[] }) {
  if (!sources.length) return null
  return (
    <section className="rep-sec" aria-labelledby="rep-sources">
      <h2 id="rep-sources" className="rep-sec-h">
        Sources
      </h2>
      <ol className="rep-sources">
        {sources.slice(0, 24).map((s, i) => {
          const host = hostName(s.url)
          return (
            <li key={`${i}:${s.url}`}>
              <a className="rep-src" href={s.url} target="_blank" rel="noopener noreferrer">
                <span className="n">{i + 1}</span>
                <Favicon host={host} size={16} />
                <span className="rep-src-t">
                  <b>{s.title || host}</b>
                  {s.title && <span>{host}</span>}
                </span>
                <Away size={16} />
              </a>
            </li>
          )
        })}
      </ol>
    </section>
  )
}
