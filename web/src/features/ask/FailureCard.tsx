import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { api, ApiError } from '../../api/client'
import type { Entry } from '../../api/types'
import { submit } from '../../composer/outbox'
import { useApp } from '../../state/store'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { Alert, Reload } from '../../ui/icons'
import { useNow } from '../../ui/Timer'
import { failureView, waitLeft } from './failure'
import './ask.css'

// Kinds after which an earlier card is no longer the latest word.
const moves = new Set(['user', 'say', 'ask', 'report', 'takeover', 'login', 'error'])

// useIsLatest says whether nothing has happened since this entry: no new
// message, and Alexa isn't at work. Only then are Try again and Resume
// offered.
export function useIsLatest(id: number) {
  return useApp((s) => s.data.session.status !== 'working' && !s.data.feed.some((e) => e.id > id && moves.has(e.kind)))
}

// A server without the endpoint (an older one) gets the same request as a
// visible message instead.
const missing = (e: unknown) => e instanceof ApiError && (e.status === 404 || e.status === 405)

type Busy = '' | 'retry' | 'repair'

// repaired says what Fix conversation did. With nothing Alexa couldn't
// read, the server sets nothing aside and doesn't carry on (after a
// refusal over something else, say), so the card mustn't claim it did.
export const repaired = (aside: string[]) => (aside.length ? 'Set aside what Alexa couldn’t read.' : 'There was nothing Alexa couldn’t read to set aside.')

// FailureCard is a run that ended badly (Appendix B.4): why, in a plain
// title, the server's detail, and the way forward: Try again, Fix
// conversation when something sent can't be read, and Details.
export function FailureCard({ entry: e, live }: { entry: Entry; live: boolean }) {
  const v = failureView(e.data?.failure, e.text)
  const [busy, setBusy] = useState<Busy>('')
  const [done, setDone] = useState<Busy>('')
  const [aside, setAside] = useState<string[]>([])
  const [err, setErr] = useState('')
  const [details, setDetails] = useState(false)

  const run = async (what: Exclude<Busy, ''>) => {
    setBusy(what)
    setErr('')
    try {
      if (what === 'repair') setAside((await api.repair()).setAside ?? [])
      else await api.retry()
      setDone(what)
    } catch (x) {
      if (what === 'retry' && missing(x)) {
        const it = await submit({ text: 'Please try that again.' }, 'ask')
        if (it.state === 'failed') setErr('Couldn’t send')
        else setDone('retry')
      } else setErr((x as Error).message || 'That didn’t work')
    } finally {
      setBusy('')
    }
  }

  const canRetry = live && (v.retry || !e.data?.failure) && done !== 'retry'
  // Details is a disclosure, not a way forward, so it reads as a link after
  // the explanation rather than as a button among the actions.
  const more = e.data?.failure && (
    <button type="button" className="linkish fail-more" aria-expanded={details} onClick={() => setDetails(!details)}>
      {details ? 'Hide details' : 'Details'}
    </button>
  )
  // What happened, in the server's words, and whether some of it got done;
  // Details follows the last of them.
  const lines = [v.body, v.partial ? 'Some of the work is done. It’s above.' : ''].filter(Boolean)
  // Details adds what the card doesn't already say: the entry's own text
  // and the server's detail, when they aren't the line above.
  const unsaid = [...new Set([e.text, e.data?.failure?.detail].map((t) => (t ?? '').trim()))].filter((t) => t && t !== v.body && t !== v.title)
  return (
    <section className={cx('fail', !live && 'past')} aria-label="Alexa stopped with a problem">
      <span className="fail-ic">
        <Alert size={18} />
      </span>
      <div className="fail-main">
        <p className="fail-title">{v.title}</p>
        {lines.map((l, i) => (
          <p key={i} className="fail-body">
            {l}
            {i === lines.length - 1 && more && <> {more}</>}
          </p>
        ))}
        {!lines.length && more && <p className="fail-body">{more}</p>}
        {(canRetry || (live && v.repair && done !== 'repair')) && (
          <div className="fail-actions">
            {canRetry && <TryAgain retryAt={v.retryAt} busy={busy} onClick={() => run('retry')} />}
            {live && v.repair && done !== 'repair' && (
              <Button size="sm" loading={busy === 'repair'} disabled={!!busy} title="Set aside the attachments Alexa can’t read, and carry on" onClick={() => run('repair')}>
                Fix conversation
              </Button>
            )}
          </div>
        )}
        {done === 'repair' && <p className="fail-body">{repaired(aside)}</p>}
        {err && (
          <p className="err" role="alert">
            {err}
          </p>
        )}
        {details && e.data?.failure && (
          <dl className="fail-details">
            <div>
              <dt>Reason</dt>
              <dd>
                <code>{e.data.failure.reason}</code>
              </dd>
            </div>
            <div>
              <dt>Can retry</dt>
              <dd>{e.data.failure.retryable ? 'Yes' : 'No'}</dd>
            </div>
            {unsaid.map((t) => (
              <div key={t}>
                <dt>Message</dt>
                <dd>{t}</dd>
              </div>
            ))}
          </dl>
        )}
      </div>
    </section>
  )
}

// TryAgain is Try again, held back with a countdown until retryAt when the
// server asked to wait. Only a countdown reads the clock, and only while it
// runs: a card without one doesn't re-render every second (every error
// card in Reflect included).
function TryAgain({ retryAt, busy, onClick }: { retryAt?: number; busy: Busy; onClick: () => void }) {
  const [counting, setCounting] = useState(() => !!waitLeft(retryAt, Date.now()))
  useEffect(() => setCounting(!!waitLeft(retryAt, Date.now())), [retryAt])
  const stopCounting = useCallback(() => setCounting(false), [])
  const button = (wait: string) => (
    <Button size="sm" variant="primary" icon={<Reload size={16} />} loading={busy === 'retry'} disabled={!!wait || !!busy} onClick={onClick}>
      {wait ? <span className="num">Try again in {wait}</span> : 'Try again'}
    </Button>
  )
  if (!counting || !retryAt) return button('')
  return <Countdown until={retryAt} onDone={stopCounting} draw={button} />
}

function Countdown({ until, onDone, draw }: { until: number; onDone: () => void; draw: (wait: string) => ReactNode }) {
  const wait = waitLeft(until, useNow())
  useEffect(() => {
    if (!wait) onDone()
  }, [wait, onDone])
  return draw(wait)
}
