import { useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Entry, Report, SessionState } from '../lib/api'
import { ago, api, hostOf } from '../lib/api'
import { Calendar, Check, Close, Download, File, Globe, HandBack, LinkIcon, Pointer, Stop, Wave } from './Icons'
import { LiveScreen, type Cursor } from './LiveScreen'
import { thinkAnim, waitAnim } from './Log'

export type Live = { url: string; cursor: Cursor | null }

// ---------- the shared browser ----------

type BrowserProps = {
  state: SessionState
  live: Live
  onTakeOver: () => void
  onHandBack: () => void
  onClose: () => void
  onUrl: (url: string) => void
}

export function BrowserPanel({ state, live, onTakeOver, onHandBack, onClose, onUrl }: BrowserProps) {
  const takeover = state.status === 'needs' && state.needs?.kind === 'takeover'
  const working = state.status === 'working'
  const title = state.control ? 'You’re in control' : takeover ? state.needs!.message : working ? state.activity || 'Working on it' : hostOf(live.url || state.browserUrl || '') || 'Browser'
  return (
    <section className="panel" aria-label="Shared browser">
      <div className="panel-head">
        <span className="panel-ic">{state.control ? <Pointer size={17} /> : working ? <Wave size="small" anim={thinkAnim} /> : takeover ? <Wave size="small" anim={waitAnim} /> : <Globe size={17} />}</span>
        <span className="title" aria-live="polite">
          {title}
        </span>
        {state.control ? (
          <button type="button" className="btn sm primary" onClick={onHandBack}>
            <HandBack size={16} />
            <span>{working || takeover ? 'Hand back' : 'Done'}</span>
          </button>
        ) : (
          <button type="button" className={`btn sm${takeover ? ' primary' : ''}`} onClick={onTakeOver}>
            <Pointer size={16} />
            <span>Take over</span>
          </button>
        )}
        <button type="button" className="icon-btn" aria-label="Close the browser" onClick={onClose}>
          <Close size={18} />
        </button>
      </div>
      <div className="panel-body browser">
        <LiveScreen control={state.control} cursor={live.cursor} url={live.url || state.browserUrl || ''} onUrl={onUrl} />
      </div>
    </section>
  )
}

// ---------- reports ----------

export function ReportPanel({ report, onClose }: { report: Report | null; onClose: () => void }) {
  const [added, setAdded] = useState(false)
  if (!report)
    return (
      <section className="panel" aria-label="Report">
        <div className="panel-head">
          <span className="title" />
          <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>
            <Close size={18} />
          </button>
        </div>
        <div className="panel-body center">
          <span className="spinner" />
        </div>
      </section>
    )
  const d = report.data || {}
  const html = report.format === 'html'
  return (
    <section className="panel" aria-label={report.title}>
      <div className="panel-head">
        <span className="panel-ic">
          <File size={17} />
        </span>
        <span className="title">
          {report.title}
          <span className="sub">{ago(report.updatedAt)}</span>
        </span>
        {d.events && d.events.length > 0 && (
          <a className="btn sm" href={api.reportCalendarURL(report.id)} download onClick={() => setAdded(true)}>
            {added ? <Check size={16} /> : <Calendar size={16} />}
            <span className="wide-only">{added ? 'Added' : d.events.length > 1 ? `Add ${d.events.length} to calendar` : 'Add to calendar'}</span>
          </a>
        )}
        <a className="icon-btn" href={api.reportDownloadURL(report.id)} download aria-label="Download" title="Download">
          <Download size={18} />
        </a>
        <button type="button" className="icon-btn" aria-label="Close the report" onClick={onClose}>
          <Close size={18} />
        </button>
      </div>
      {html ? (
        <div className="panel-body frame">
          <iframe title={report.title} sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox" referrerPolicy="no-referrer" srcDoc={withCSP(report.content || '')} />
        </div>
      ) : (
        <div className="panel-body doc">
          <article className="report">
            <h1>{report.title}</h1>
            {d.items && d.items.length > 0 && (
              <ul className="items">
                {d.items.map((it, i) => (
                  <li key={i}>
                    <span className="l">{it.label}</span>
                    {it.detail && <span className="d">{it.detail}</span>}
                  </li>
                ))}
              </ul>
            )}
            {report.content && (
              <div className="md">
                <Markdown
                  remarkPlugins={[remarkGfm]}
                  components={{
                    a: ({ href, children }) => (
                      <a href={href} target="_blank" rel="noopener noreferrer">
                        {children}
                      </a>
                    ),
                  }}
                >
                  {withoutTitle(report.content, report.title)}
                </Markdown>
              </div>
            )}
            <Attachments report={report} />
          </article>
        </div>
      )}
    </section>
  )
}

function Attachments({ report }: { report: Report }) {
  const d = report.data || {}
  const files = d.files || []
  const sources = d.sources || []
  return (
    <>
      {files.filter(isImage).map((f) => (
        <a key={'img-' + f} href={api.fileURL(f, true)} target="_blank" rel="noopener noreferrer">
          <img className="result-img" src={api.fileURL(f, true)} alt={f.split('/').pop()} loading="lazy" />
        </a>
      ))}
      {files.length > 0 && (
        <div className="files">
          {files.map((f) => (
            <a key={f} className="file-row" href={api.fileURL(f)} download>
              <File size={18} />
              <span className="n">{f.split('/').pop()}</span>
              <Download size={16} />
            </a>
          ))}
        </div>
      )}
      {sources.length > 0 && (
        <div className="sources">
          {sources.slice(0, 12).map((l) => (
            <a key={l.url} className="source" href={l.url} target="_blank" rel="noopener noreferrer">
              <LinkIcon size={13} />
              <span>{l.title || hostOf(l.url)}</span>
            </a>
          ))}
        </div>
      )}
    </>
  )
}

// withoutTitle drops a leading heading that just repeats the report's
// title, which is already shown above the content.
function withoutTitle(md: string, title: string) {
  const m = md.match(/^\s*#{1,2}\s+(.+)\n+/)
  if (!m) return md
  const norm = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '')
  const a = norm(m[1])
  const b = norm(title)
  const words = (s: string) => new Set(s.toLowerCase().split(/[^a-z0-9]+/).filter((w) => w.length > 2))
  const wa = words(m[1])
  const wb = words(title)
  const shared = [...wa].filter((w) => wb.has(w)).length
  return a === b || shared >= Math.min(wa.size, wb.size) * 0.6 ? md.slice(m[0].length) : md
}

const isImage = (f: string) => /\.(png|jpe?g|webp|gif|svg)$/i.test(f)

// HTML reports run in a sandboxed frame with no access to Alexa, the
// network (beyond images, fonts and well-known script CDNs) or the user's
// session.
const csp =
  "default-src 'none'; img-src data: blob: https:; media-src data: blob: https:; style-src 'unsafe-inline' https://fonts.googleapis.com; font-src data: https://fonts.gstatic.com; " +
  "script-src 'unsafe-inline' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com https://unpkg.com"

function withCSP(html: string) {
  const meta = `<meta http-equiv="Content-Security-Policy" content="${csp}">`
  if (/<head[^>]*>/i.test(html)) return html.replace(/<head[^>]*>/i, (m) => m + meta)
  return meta + html
}

// ---------- the caption under a panel on small screens ----------

type CaptionProps = {
  state: SessionState
  latest: Entry | null
  onAnswer: (text: string) => void
  onTakeOver: () => void
  onHandBack: () => void
  onStop: () => void
}

// Caption keeps the conversation in view while a panel fills a small
// screen: Alexa's question, what it's doing, or the last thing it said.
export function Caption({ state, latest, onAnswer, onTakeOver, onHandBack, onStop }: CaptionProps) {
  const nd = state.status === 'needs' ? state.needs : null
  if (nd?.kind === 'question')
    return (
      <div className="caption ask">
        <p className="q">{nd.message}</p>
        {nd.options && nd.options.length > 0 && (
          <div className="opts">
            {nd.options.map((o, i) => (
              <button key={o} type="button" className={`btn xs${i === 0 ? ' primary' : ''}`} onClick={() => onAnswer(o)}>
                {o}
              </button>
            ))}
          </div>
        )}
      </div>
    )
  if (nd?.kind === 'takeover')
    return (
      <div className="caption">
        <Wave size="small" anim={waitAnim} />
        <p>{nd.message}</p>
        {state.control ? (
          <button type="button" className="btn xs primary" onClick={onHandBack}>
            Hand back
          </button>
        ) : (
          <button type="button" className="btn xs primary" onClick={onTakeOver}>
            Take over
          </button>
        )}
      </div>
    )
  if (state.status === 'working')
    return (
      <div className="caption">
        <Wave size="small" anim={state.control ? waitAnim : thinkAnim} />
        <p>{state.control ? 'You’re in control' : state.activity || 'Working on it'}</p>
        <button type="button" className="icon-btn sm muted" aria-label="Stop" onClick={onStop}>
          <Stop size={16} />
        </button>
      </div>
    )
  if (latest) return <Said key={latest.id} text={latest.text} />
  return null
}

// Said is Alexa's last message, clamped to three lines until tapped.
function Said({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  return (
    <button type="button" className={`caption said${open ? ' open' : ''}`} aria-expanded={open} onClick={() => setOpen(!open)}>
      <p>{text}</p>
    </button>
  )
}
