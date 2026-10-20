import { Fragment, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import type { Entry, SessionState } from '../lib/api'
import { api, clock, day, elapsed } from '../lib/api'
import { Alert, Check, Chevron, Close, Eye, File, HandBack, Pointer, Stop, Wave, You } from './Icons'

export const thinkAnim = 'wThink 1.1s ease-in-out infinite'
export const waitAnim = 'wWait 2.4s ease-in-out infinite'

type Props = {
  feed: Entry[]
  state: SessionState
  hasMore: boolean
  greeting: string
  onMore: () => void
  onAnswer: (text: string) => void
  onTakeOver: () => void
  onHandBack: () => void
  onStop: () => void
  onOpenReport: (id: string) => void
  onShowBrowser?: () => void
}

type Block = { key: string; time?: number; entry?: Entry; steps?: Entry[] }

// A pause this long between entries gets a time divider.
const gap = 45 * 60e3

function toBlocks(feed: Entry[]): Block[] {
  const out: Block[] = []
  let prev = 0
  for (const e of feed) {
    if (!prev || e.createdAt - prev > gap) out.push({ key: `t${e.id}`, time: e.createdAt })
    prev = e.createdAt
    const last = out[out.length - 1]
    if (e.kind === 'step') {
      if (last?.steps) last.steps.push(e)
      else out.push({ key: `s${e.id}`, steps: [e] })
      continue
    }
    out.push({ key: `e${e.id}`, entry: e })
  }
  return out
}

// Log is the one ongoing conversation: what the user said, what Alexa said
// (only through its say tool), its questions and reports, and the steps it
// took in between, folded away once done.
export function Log(p: Props) {
  const { feed, state } = p
  const box = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const prev = useRef({ first: 0, height: 0 })
  const blocks = toBlocks(feed)
  const working = state.status === 'working'

  // Stay pinned to the newest entry unless the user scrolled up; keep the
  // view steady when older entries load above.
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const first = feed[0]?.id || 0
    if (prev.current.first && first < prev.current.first) el.scrollTop += el.scrollHeight - prev.current.height
    else if (atBottom.current) el.scrollTop = el.scrollHeight
    prev.current = { first, height: el.scrollHeight }
  })

  return (
    <div
      ref={box}
      className="log"
      onScroll={(e) => {
        const el = e.currentTarget
        atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
      }}
    >
      <div className="log-inner">
        {p.hasMore && (
          <button type="button" className="btn xs more" onClick={p.onMore}>
            Show earlier
          </button>
        )}
        {feed.length === 0 && !working && (
          <div className="idle">
            <p className="greet">{p.greeting}</p>
          </div>
        )}
        {blocks.map((b, i) => (
          <Fragment key={b.key}>
            {b.time && <div className="when">{`${day(b.time)} ${clock(b.time)}`}</div>}
            {b.steps && <Steps steps={b.steps} live={working && i === blocks.length - 1} />}
            {b.entry && <Item e={b.entry} {...p} />}
          </Fragment>
        ))}
        {working && (
          <div className="working" aria-live="polite">
            <Wave size="small" anim={state.control ? waitAnim : thinkAnim} />
            <span className="act">{state.control ? 'You’re in control' : state.activity || 'Working on it'}</span>
            {p.onShowBrowser && (
              <button type="button" className="icon-btn sm muted" aria-label="Show the browser" title="Show the browser" onClick={p.onShowBrowser}>
                <Eye size={17} />
              </button>
            )}
            <button type="button" className="icon-btn sm muted" aria-label="Stop" title="Stop" onClick={p.onStop}>
              <Stop size={17} />
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

function Item({ e, state, onAnswer, onTakeOver, onHandBack, onStop, onOpenReport }: Props & { e: Entry }) {
  switch (e.kind) {
    case 'user':
      return (
        <div className="msg you">
          {e.text && <p>{e.text}</p>}
          {e.data?.attachments?.map((a) => (
            <a key={a.path} className="att" href={api.fileURL(a.path, true)} target="_blank" rel="noopener noreferrer">
              <File size={14} />
              <span>{a.name}</span>
            </a>
          ))}
        </div>
      )
    case 'say':
      return (
        <div className="msg agent">
          <p>{linkify(e.text)}</p>
        </div>
      )
    case 'ask': {
      const opts = e.data?.options || []
      if (e.status !== 'open') return <div className="msg agent"><p>{e.text}</p></div>
      return (
        <section className="ask" aria-label="Alexa is asking">
          <p className="q">{e.text}</p>
          {opts.length > 0 && (
            <div className="opts">
              {opts.map((o, i) => (
                <button key={o} type="button" className={`btn sm${i === 0 ? ' primary' : ''}`} onClick={() => onAnswer(o)}>
                  {o}
                </button>
              ))}
            </div>
          )}
          <p className="hint">
            {opts.length ? 'Pick one or answer below.' : 'Answer below.'}{' '}
            <button type="button" className="linkish" onClick={onStop}>
              Dismiss
            </button>
          </p>
        </section>
      )
    }
    case 'takeover':
      if (e.status !== 'open') return <div className="msg agent muted"><p>{e.text}</p></div>
      return (
        <section className="ask" aria-label="Alexa needs you in the browser">
          <div className="eyebrow">
            <Pointer size={15} />
            <span>Your turn in the browser</span>
          </div>
          <p className="q">{e.text}</p>
          <div className="opts">
            {state.control ? (
              <button type="button" className="btn sm primary" onClick={onHandBack}>
                <HandBack size={16} />
                <span>Hand back</span>
              </button>
            ) : (
              <button type="button" className="btn sm primary" onClick={onTakeOver}>
                <Pointer size={16} />
                <span>Take over</span>
              </button>
            )}
            <button type="button" className="btn sm" onClick={onStop}>
              Not now
            </button>
          </div>
        </section>
      )
    case 'report': {
      const id = e.data?.id
      return (
        <button type="button" className="rep-card" onClick={() => id && onOpenReport(id)} disabled={!id}>
          <span className="rep-ic">
            <File size={18} />
          </span>
          <span className="rep-t">
            <b>{e.text}</b>
            <span>{e.data?.updated ? 'Report updated' : 'Report'}</span>
          </span>
          <span className="rep-go">
            <Chevron size={16} />
          </span>
        </button>
      )
    }
    case 'error':
      return (
        <div className="msg error">
          <Alert size={16} />
          <p>{e.text}</p>
        </div>
      )
    case 'note':
      return <div className="note">{e.text}</div>
  }
  return null
}

// Steps shows what Alexa did. While it works the steps so far are listed
// (the current one is the working line below); afterwards they fold into
// one line.
function Steps({ steps, live }: { steps: Entry[]; live: boolean }) {
  const [open, setOpen] = useState(false)
  const [all, setAll] = useState(false)
  const done = steps.filter((s) => s.status !== 'running')
  if (live) {
    if (!done.length) return null
    const shown = all ? done : done.slice(-4)
    return (
      <div className="steps-log">
        {shown.length < done.length && (
          <button type="button" className="linkish more-steps" onClick={() => setAll(true)}>
            {done.length - shown.length} earlier {done.length - shown.length === 1 ? 'step' : 'steps'}
          </button>
        )}
        <StepLines steps={shown} />
      </div>
    )
  }
  const first = steps[0]
  const lastStep = steps[steps.length - 1]
  const failed = steps.some((s) => s.status === 'failed')
  return (
    <div className="steps-log">
      <button type="button" className="steps-sum" aria-expanded={open} onClick={() => setOpen(!open)}>
        {failed ? <Close size={14} /> : <Check size={14} />}
        <span>
          {steps.length === 1 ? label(first) : `${steps.length} steps`} · {elapsed(first.createdAt, lastStep.updatedAt)}
        </span>
        <span className={`chev${open ? ' up' : ''}`}>
          <Chevron size={14} />
        </span>
      </button>
      {open && <StepLines steps={steps} />}
    </div>
  )
}

function StepLines({ steps }: { steps: Entry[] }) {
  return (
    <ol className="steps">
      {steps.map((s) => {
        const you = s.text === 'You took over'
        return (
          <li key={s.id}>
            <span className="ic">{you ? <You size={13} /> : s.status === 'failed' ? <Close size={13} /> : <Check size={13} />}</span>
            <span className={`lbl${s.status === 'failed' ? ' failed' : ''}${you ? ' you' : ''}`}>{label(s)}</span>
          </li>
        )
      })}
    </ol>
  )
}

const label = (s: Entry) => (s.status === 'done' && s.doneText ? s.doneText : s.text)

// linkify turns bare URLs in Alexa's messages into links.
function linkify(text: string): ReactNode[] {
  const parts = text.split(/(https?:\/\/[^\s)]+[^\s).,;:!?'"])/g)
  return parts.map((part, i) =>
    i % 2 ? (
      <a key={i} href={part} target="_blank" rel="noopener noreferrer">
        {part.replace(/^https?:\/\/(www\.)?/, '')}
      </a>
    ) : (
      part
    ),
  )
}
