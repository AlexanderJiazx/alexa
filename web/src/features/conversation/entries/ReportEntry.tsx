import { api } from '../../../api/client'
import '../../../panels/report/report.css'
import { Page } from '../../../panels/report/icons'
import { excerpt, meta } from '../../../panels/report/text'
import { openReport } from '../../../state/actions'
import { useReport, useSurface } from '../../../state/selectors'
import { cx } from '../../../ui/cx'
import { Chevron } from '../../../ui/icons'
import type { EntryProps } from '../registry'

// ReportEntry is the card for a report Alexa wrote: its title on up to two
// lines, the takeaway or the start of its lede, and what's inside. It opens
// the report in the panel, and on the main screen says when it's the one
// showing there (Reflect covers the panel, so it doesn't).
export function ReportEntry({ entry: e, mode, compact }: EntryProps) {
  // The report's id, whether it replaced an earlier version, the takeaway
  // Alexa wrote for its card and a preview of its blocks (WP-03).
  const d = e.data
  const id = d?.id
  const report = useReport(id)
  const surface = useSurface()
  const open = mode !== 'history' && !!id && surface.kind === 'report' && surface.id === id
  const html = report?.format === 'html'
  const p = d?.preview
  const thumb = p?.thumbs?.[0]
  const line = compact ? '' : excerpt(d?.takeaway, report)
  const about = meta(report, p?.kinds)
  return (
    <button type="button" className={cx('rep-card', open && 'open', compact && 'compact')} onClick={() => id && openReport(id)} disabled={!id} aria-current={open || undefined}>
      {thumb ? (
        <span className="rep-thumb">
          <img src={api.imgURL(thumb)} alt="" loading="lazy" decoding="async" />
        </span>
      ) : (
        <span className={cx('rep-paper', html && 'page')} aria-hidden="true">
          {html ? <Page size={18} /> : <i />}
        </span>
      )}
      <span className="rep-t">
        <b>{e.text}</b>
        {line && <span className="rep-x">{line}</span>}
        <span className="rep-m">
          <span>{d?.updated ? 'Report updated' : 'Report'}</span>
          {p?.stat && (
            <span className="rep-m-stat">
              {p.stat.label} <b>{p.stat.value}</b>
              {p.stat.delta && <span> {p.stat.delta}</span>}
            </span>
          )}
          {about && about.split(' · ').map((a) => <span key={a}>{a}</span>)}
        </span>
      </span>
      {open ? (
        <span className="rep-open">Showing</span>
      ) : (
        <span className="rep-go" aria-hidden="true">
          <Chevron size={16} />
        </span>
      )}
    </button>
  )
}
