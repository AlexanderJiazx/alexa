import { useEffect, useRef, useState } from 'react'
import type { Report } from '../../api/types'
import { notify } from '../../sheets/toasts'
import { closePanel, loadReport, send } from '../../state/actions'
import { useReport } from '../../state/selectors'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { IconButton } from '../../ui/IconButton'
import { Close, Report as ReportGlyph } from '../../ui/icons'
import { Skeleton } from '../../ui/Skeleton'
import type { PanelProps } from '../registry'
import { HtmlFrame } from './HtmlFrame'
import { ReportDoc } from './ReportDoc'
import { ReportHead } from './ReportHead'
import './report.css'

// sendFromReport sends what an HTML report passed to alexa.send as the
// user's own, visible message, marked as sent from the report. Below
// 1080px the panel covers the conversation, so a toast says it went. It
// resolves false when the message couldn't go, so a reply card frees its
// buttons.
function sendFromReport(text: string): Promise<boolean> {
  return send(text, [], false, 'report').then((ok) => {
    if (ok && typeof matchMedia === 'function' && matchMedia('(max-width: 1079px)').matches) notify({ title: 'Sent from the report', body: text, tone: 'ok' })
    return ok
  })
}

// ReportPanel shows a report Alexa wrote: a document, or a sandboxed HTML
// page. It fetches the report if this client hasn't seen it yet.
export function ReportPanel({ id }: PanelProps) {
  const report = useReport(id)
  const [failed, setFailed] = useState(false)
  const [tries, setTries] = useState(0)
  const have = !!report
  useEffect(() => {
    if (have) return
    setFailed(false)
    // A false means it's gone or couldn't be fetched, and the panel says
    // so instead of spinning.
    loadReport(id).then((ok) => !ok && setFailed(true))
  }, [id, have, tries])
  if (!report) return <ReportLoading failed={failed} onRetry={() => setTries((n) => n + 1)} onClose={closePanel} />
  return <ReportView report={report} />
}

type ViewProps = {
  report: Report
  onClose?: () => void
  onSend?: (text: string) => void | Promise<boolean>
}

// ReportView is a report in the panel. The head picks up the title once
// the document's own title has scrolled under it, and lifts off the page
// with a hairline once the page scrolls.
export function ReportView({ report, onClose = closePanel, onSend = sendFromReport }: ViewProps) {
  const html = report.format === 'html'
  const scroller = useRef<HTMLDivElement>(null)
  const top = useRef<HTMLSpanElement>(null)
  const title = useRef<HTMLHeadingElement>(null)
  const [lifted, setLifted] = useState(false)
  const [scrolledTitle, setScrolledTitle] = useState(false)
  useEffect(() => {
    const root = scroller.current
    if (!root || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver(
      (list) => {
        for (const e of list) {
          if (e.target === top.current) setLifted(!e.isIntersecting)
          else if (e.target === title.current) setScrolledTitle(!e.isIntersecting && e.boundingClientRect.top < (e.rootBounds?.top ?? 0))
        }
      },
      { root },
    )
    if (top.current) io.observe(top.current)
    if (title.current) io.observe(title.current)
    return () => io.disconnect()
  }, [html, report.id])
  return (
    <section className="panel rep-panel" aria-label={report.title}>
      <ReportHead report={report} titled={html || scrolledTitle} lifted={lifted} onClose={onClose} />
      <div ref={scroller} className={cx('panel-body', html ? 'rep-frame' : 'doc rep-doc')}>
        <span ref={top} className="rep-top-edge" aria-hidden="true" />
        {html ? <HtmlFrame report={report} onSend={onSend} /> : <ReportDoc report={report} titleRef={title} onSend={onSend} />}
      </div>
    </section>
  )
}

type LoadingProps = { failed?: boolean; onRetry?: () => void; onClose: () => void }

// ReportLoading holds the report's place while it loads: the shape of a
// title and a few paragraphs. If it can't be had, it says so.
export function ReportLoading({ failed, onRetry, onClose }: LoadingProps) {
  return (
    <section className="panel rep-panel" aria-label="Report" aria-busy={!failed}>
      <div className="panel-head rep-head">
        <span className="panel-ic">
          <ReportGlyph size={18} />
        </span>
        <span className="rep-slot">
          <span className="rep-kicker">Report</span>
        </span>
        <IconButton label="Close the report" size="sm" onClick={onClose}>
          <Close size={18} />
        </IconButton>
      </div>
      <div className="panel-body doc rep-doc">
        {failed ? (
          <div className="rep-gone" role="alert">
            <b>Couldn’t open this report</b>
            <p>It may have been deleted, or the connection dropped.</p>
            <div className="row-btns">
              {onRetry && <Button onClick={onRetry}>Try again</Button>}
              <Button variant="ghost" onClick={onClose}>
                Close
              </Button>
            </div>
          </div>
        ) : (
          <div className="report rep-skel">
            <Skeleton width={180} height={12} />
            <Skeleton width="72%" height={40} radius="sm" className="rep-skel-title" />
            <Skeleton lines={2} className="rep-skel-lede" />
            <Skeleton lines={4} className="rep-skel-p" />
            <Skeleton lines={3} className="rep-skel-p" />
          </div>
        )}
      </div>
    </section>
  )
}
