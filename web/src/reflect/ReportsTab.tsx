import { api } from '../api/client'
import type { Report } from '../api/types'
import { preview } from '../features/home/home'
import { ago } from '../lib/format'
import { failed, LoadError, useList } from '../sheets/parts'
import { removeLater, usePending } from '../sheets/undo'
import { openReport } from '../state/actions'
import { useChanged } from '../state/selectors'
import { cardClass } from '../ui/Card'
import { cx } from '../ui/cx'
import { EmptyState } from '../ui/EmptyState'
import { IconButton } from '../ui/IconButton'
import { Download, Report as ReportIcon, Trash } from '../ui/icons'
import { Skeleton } from '../ui/Skeleton'

const week = 7 * 864e5

// Reports is everything Alexa wrote up, as document cards, newest first:
// this week's, then the earlier ones.
export function Reports({ query }: { query: string }) {
  const changed = useChanged('reports')
  const gone = useChanged('reportDeleted')
  const { list, error, reload } = useList(api.reports, [changed, gone])
  const pending = usePending()
  const q = query.trim().toLowerCase()
  const shown = (list ?? []).filter((r) => !pending.has(`report:${r.id}`) && (!q || r.title.toLowerCase().includes(q)))
  const recent = shown.filter((r) => Date.now() - r.updatedAt < week)
  const earlier = shown.filter((r) => Date.now() - r.updatedAt >= week)

  const remove = (r: Report) =>
    removeLater({
      key: `report:${r.id}`,
      title: 'Report deleted',
      body: r.title,
      commit: () => api.deleteReport(r.id),
      done: reload,
      failed: failed('Couldn’t delete that report'),
    })

  return (
    <div className="reflect-scroll">
      <div className="reflect-pane">
        {error && !list ? (
          <LoadError message={error} onRetry={reload} />
        ) : list === null ? (
          <div className="report-grid" aria-busy="true" aria-label="Loading">
            {[0, 1, 2].map((i) => (
              <div key={i} className={cx(cardClass(false, 'md'), 'report-card')}>
                <Skeleton width={56} height={12} />
                <Skeleton lines={2} />
              </div>
            ))}
          </div>
        ) : shown.length === 0 ? (
          q ? (
            <p className="empty-note">No report is called anything like “{query.trim()}”.</p>
          ) : (
            <EmptyState icon={<ReportIcon size={28} />} title="No reports yet" body="Ask Alexa to research, compare or plan something, and what it writes up is kept here." />
          )
        ) : (
          <>
            {recent.length > 0 && <Shelf title="This week" reports={recent} onDelete={remove} />}
            {earlier.length > 0 && <Shelf title="Earlier" reports={earlier} onDelete={remove} />}
          </>
        )}
      </div>
    </div>
  )
}

// A report is called what its own panel calls it (ReportHead), and its card
// says what's in it the way Home's does (preview).
const kindOf = (r: Pick<Report, 'format'>) => (r.format === 'html' ? 'Page' : 'Report')

function Excerpt({ r }: { r: Report }) {
  const p = preview(r)
  return p ? <p className="report-excerpt">{p}</p> : null
}

function Shelf({ title, reports, onDelete }: { title: string; reports: Report[]; onDelete: (r: Report) => void }) {
  return (
    <section className="shelf">
      <h2 className="shelf-title">{title}</h2>
      <ul className="report-grid">
        {reports.map((r) => (
          <li key={r.id} className={cx(cardClass(true, 'md'), 'report-card')}>
            <span className="report-meta">
              <ReportIcon size={14} />
              <span>{kindOf(r)}</span>
            </span>
            <button type="button" className="report-open" onClick={() => openReport(r.id)}>
              <span className="report-title">{r.title}</span>
            </button>
            <Excerpt r={r} />
            <span className="report-when num">
              {r.updatedAt !== r.createdAt ? 'Updated ' : ''}
              {ago(r.updatedAt)}
            </span>
            <span className="report-actions">
              <a className="icon-btn sm muted" href={api.reportDownloadURL(r.id)} download aria-label={`Download ${r.title}`} title="Download">
                <Download size={16} />
              </a>
              <IconButton size="sm" tone="muted" label={`Delete ${r.title}`} title="Delete" onClick={() => onDelete(r)}>
                <Trash size={16} />
              </IconButton>
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}
