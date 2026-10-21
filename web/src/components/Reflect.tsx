import { useEffect, useRef, useState } from 'react'
import type { Entry, FileInfo, Report, SessionState } from '../lib/api'
import { ago, api, fmtSize } from '../lib/api'
import { Back, Download, Trash } from './Icons'
import { Log } from './Log'

export type ReflectTab = 'conversation' | 'reports' | 'files'

type Props = {
  tab: ReflectTab
  onTab: (t: ReflectTab) => void
  leaving: boolean
  feed: Entry[]
  hasMore: boolean
  state: SessionState
  onMore: () => void
  onOpenReport: (id: string) => void
  onClose: () => void
}

const tabs: [ReflectTab, string][] = [
  ['conversation', 'Conversation'],
  ['reports', 'Reports'],
  ['files', 'Files'],
]

// Reflect is where the past lives: the whole conversation with Alexa, the
// reports it wrote and the files in the workspace. The main screen only
// shows what's happening now.
export function Reflect({ tab, onTab, leaving, feed, hasMore, state, onMore, onOpenReport, onClose }: Props) {
  const back = useRef<HTMLButtonElement>(null)
  // The page takes the keyboard from the composer behind it.
  useEffect(() => back.current?.focus(), [])
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === 'Escape' && !document.querySelector('.sheet') && onClose()
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [onClose])
  const none = () => {}
  return (
    <section className={`reflect${leaving ? ' leaving' : ''}`} role="dialog" aria-label="Reflect">
      <header className="reflect-head">
        <button ref={back} type="button" className="icon-btn" aria-label="Back to Alexa" onClick={onClose}>
          <Back size={20} />
        </button>
        <h1>Reflect</h1>
        <div className="seg" role="tablist" aria-label="What to look back on">
          {tabs.map(([t, label]) => (
            <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => onTab(t)}>
              {label}
            </button>
          ))}
        </div>
      </header>
      <div className="reflect-body">
        {tab === 'conversation' && (
          <Log mode="history" entries={feed} state={state} hasMore={hasMore} onMore={onMore} onOpenReport={onOpenReport} onAnswer={none} onTakeOver={none} onHandBack={none} onStop={none} />
        )}
        {tab === 'reports' && <Reports onOpen={onOpenReport} />}
        {tab === 'files' && <Files />}
      </div>
    </section>
  )
}

function Reports({ onOpen }: { onOpen: (id: string) => void }) {
  const [list, setList] = useState<Report[] | null>(null)
  const [err, setErr] = useState('')
  const load = () => api.reports().then(setList).catch((e) => setErr(e.message))
  useEffect(() => {
    load()
  }, [])
  return (
    <div className="reflect-pane">
      <div>
        <p className="empty-note" style={{ paddingTop: 0 }}>
          Everything Alexa wrote up for you: research, plans, comparisons and results.
        </p>
        {err && <p className="err">{err}</p>}
        {list === null ? (
          <span className="spinner" />
        ) : list.length === 0 ? (
          <p className="empty-note">No reports yet. Ask Alexa to research or plan something.</p>
        ) : (
          <ul className="list">
            {list.map((r) => (
              <li key={r.id}>
                <button type="button" className="rowlink main" onClick={() => onOpen(r.id)}>
                  <div className="t1">{r.title}</div>
                  <div className="t2">
                    {r.updatedAt !== r.createdAt ? 'Updated ' : ''}
                    {ago(r.updatedAt)}
                  </div>
                </button>
                <a className="icon-btn muted" style={{ width: 36, height: 36 }} href={api.reportDownloadURL(r.id)} download aria-label={`Download ${r.title}`}>
                  <Download size={16} />
                </a>
                <button
                  type="button"
                  className="icon-btn muted"
                  style={{ width: 36, height: 36 }}
                  aria-label={`Delete ${r.title}`}
                  onClick={async () => {
                    if (!confirm(`Delete “${r.title}”?`)) return
                    await api.deleteReport(r.id).catch((e) => setErr(e.message))
                    load()
                  }}
                >
                  <Trash size={16} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function Files() {
  const [list, setList] = useState<FileInfo[] | null>(null)
  const [err, setErr] = useState('')
  const load = () =>
    api
      .files()
      .then((f) => setList(f.filter((x) => !x.isDir)))
      .catch((e) => setErr(e.message))
  useEffect(() => {
    load()
  }, [])
  return (
    <div className="reflect-pane">
      <div>
        <p className="empty-note" style={{ paddingTop: 0 }}>
          Your private workspace: uploads, downloads and files Alexa creates.
        </p>
        <label className="btn sm" style={{ marginBottom: 8 }}>
          Upload
          <input
            type="file"
            multiple
            className="sr"
            onChange={async (e) => {
              for (const f of Array.from(e.target.files || [])) await api.upload(f).catch((x) => setErr(x.message))
              load()
            }}
          />
        </label>
        {err && <p className="err">{err}</p>}
        {list === null ? (
          <span className="spinner" />
        ) : list.length === 0 ? (
          <p className="empty-note">No files yet.</p>
        ) : (
          <ul className="list">
            {list.map((f) => (
              <li key={f.path}>
                <a className="main" href={api.fileURL(f.path, true)} target="_blank" rel="noopener noreferrer" style={{ textDecoration: 'none' }}>
                  <div className="t1">{f.path}</div>
                  <div className="t2">
                    {fmtSize(f.size)} · {ago(f.modTime)}
                  </div>
                </a>
                <a className="icon-btn muted" style={{ width: 36, height: 36 }} href={api.fileURL(f.path)} download aria-label={`Download ${f.path}`}>
                  <Download size={16} />
                </a>
                <button
                  type="button"
                  className="icon-btn muted"
                  style={{ width: 36, height: 36 }}
                  aria-label={`Delete ${f.path}`}
                  onClick={async () => {
                    await api.deleteFile(f.path)
                    load()
                  }}
                >
                  <Trash size={16} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
