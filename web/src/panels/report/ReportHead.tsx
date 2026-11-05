import { useEffect, useRef, useState } from 'react'
import { api } from '../../api/client'
import type { Report } from '../../api/types'
import { FollowChip } from '../../features/caption/FollowChip'
import { ago } from '../../lib/format'
import { toast } from '../../state/actions'
import { buttonClass } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { IconButton } from '../../ui/IconButton'
import { Calendar, Check, Close, Copy, Download, Report as ReportGlyph } from '../../ui/icons'
import { announce } from '../../ui/LiveAnnouncer'
import { useNow } from '../../ui/Timer'
import { Page } from './icons'
import { copyFailure } from './text'

type Props = {
  report: Report
  // titled shows the report's title in the head: always for an HTML page,
  // and for a document once its own title has scrolled away.
  titled: boolean
  // lifted draws the hairline under the head once the page scrolls.
  lifted: boolean
  onClose: () => void
}

// Since is how long ago the report was written, the way every report card
// says it (ago), kept current.
function Since({ ts }: { ts: number }) {
  useNow()
  return <time dateTime={new Date(ts).toISOString()}>{ago(ts)}</time>
}

// copyText puts the report on the clipboard as markdown: the server's
// download, which also writes out its blocks. The clipboard is asked for
// at once, within the click, and given the text when it arrives (Safari
// refuses a write that comes after an await).
async function copyText(id: string) {
  const text = () =>
    fetch(api.reportDownloadURL(id), { credentials: 'same-origin' }).then((r) => {
      if (!r.ok) throw new Error(r.statusText || `HTTP ${r.status}`)
      return r.text()
    })
  if (typeof ClipboardItem !== 'undefined' && navigator.clipboard?.write) {
    await navigator.clipboard.write([new ClipboardItem({ 'text/plain': text().then((t) => new Blob([t], { type: 'text/plain' })) })])
  } else {
    await navigator.clipboard.writeText(await text())
  }
}

// ReportHead is the panel's head: what this is and when it was written,
// the title once it has scrolled away, and the report's actions.
export function ReportHead({ report, titled, lifted, onClose }: Props) {
  const html = report.format === 'html'
  const events = report.data?.events ?? []
  const [copied, setCopied] = useState(false)
  const [added, setAdded] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  const copy = () => {
    copyText(report.id).then(
      () => {
        setCopied(true)
        announce('Copied as Markdown')
        clearTimeout(timer.current)
        timer.current = setTimeout(() => setCopied(false), 1600)
      },
      (e) => toast('Couldn’t copy the report', copyFailure(e, !!navigator.clipboard)),
    )
  }
  const kind = html ? 'Page' : 'Report'
  return (
    <div className={cx('panel-head rep-head', lifted && 'lifted', titled && 'titled')}>
      <span className="panel-ic">{html ? <Page size={18} /> : <ReportGlyph size={18} />}</span>
      <span className="rep-slot">
        <span className="rep-kicker">
          {kind}
          <span className="rep-when">
            <span className="dot" aria-hidden="true">
              ·
            </span>
            <Since ts={report.updatedAt} />
          </span>
        </span>
        <span className="rep-title" aria-hidden={!titled}>
          {report.title}
        </span>
      </span>
      {events.length > 0 && (
        <a className={cx(buttonClass('secondary', 'sm'), 'rep-cal')} href={api.reportCalendarURL(report.id)} download onClick={() => setAdded(true)}>
          {added ? <Check size={16} /> : <Calendar size={16} />}
          <span className="rep-cal-l">{added ? 'Added' : events.length > 1 ? `Add ${events.length} to calendar` : 'Add to calendar'}</span>
        </a>
      )}
      {!html && (
        <IconButton label={copied ? 'Copied' : 'Copy as Markdown'} size="sm" onClick={copy}>
          {copied ? <Check size={18} /> : <Copy size={18} />}
        </IconButton>
      )}
      <a className="icon-btn sm" href={api.reportDownloadURL(report.id)} download aria-label={html ? 'Download .html' : 'Download .md'} title={html ? 'Download .html' : 'Download .md'}>
        <Download size={18} />
      </a>
      <FollowChip />
      <IconButton label="Close the report" size="sm" onClick={onClose}>
        <Close size={18} />
      </IconButton>
    </div>
  )
}
