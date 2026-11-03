import { shallow } from '../state/store'
import { Button } from '../ui/Button'
import { cx } from '../ui/cx'
import { Alert, Check, Close, Stop } from '../ui/icons'
import { Speaker, Waiting } from './icons'
import { dismiss, retry, useOutbox, type Item } from './outbox'
import { stopSpeaking, useSpeaking } from './speech'

export type ReceiptProps = {
  working: boolean
  // typing is true while there's text Alexa would get mid-run.
  typing: boolean
  onInterrupt: () => void
  onRetried: (it: Item) => void
}

export type Row = { key: string; tone?: 'bad' | 'ok'; icon: React.ReactNode; text: React.ReactNode; action?: React.ReactNode; close?: () => void }

// Long and short say the same thing; phones get the short one, so the line
// isn't cut off before it makes sense.
const both = (long: string, short: string) => (
  <>
    <span className="rc-long">{long}</span>
    <span className="rc-short">{short}</span>
  </>
)

// pick is the one receipt worth showing now, most urgent first: a message
// that couldn't go, what Enter will do mid-run, Alexa reading aloud, a slow
// send, then queued and seen.
export function receiptFor(items: Item[], p: ReceiptProps, speaking: boolean): Row | null {
  const failed = [...items].reverse().find((x) => x.state === 'failed')
  if (failed)
    return {
      key: `f${failed.clientId}`,
      tone: 'bad',
      icon: <Alert size={16} />,
      text: failed.error && failed.error !== 'Couldn’t send' ? `Couldn’t send. ${failed.error}` : 'Couldn’t send.',
      action: (
        <Button
          size="sm"
          variant="ghost"
          onClick={() =>
            retry(failed.clientId).then((it) => {
              if (it) p.onRetried(it)
            })
          }
        >
          Retry
        </Button>
      ),
      close: () => dismiss(failed.clientId),
    }
  if (p.working && p.typing)
    return {
      key: 'typing',
      icon: <Waiting size={16} />,
      text: both('Alexa will read this after the current step.', 'Waits for this step.'),
      action: (
        <Button size="sm" variant="ghost" icon={<Stop size={14} />} className="rc-interrupt" title="Stop the current step and do this instead (⌘↵)" onClick={p.onInterrupt}>
          <span className="rc-long">Stop and do this instead</span>
          <span className="rc-short">Do this now</span>
        </Button>
      ),
    }
  if (speaking)
    return {
      key: 'speaking',
      icon: <Speaker size={16} />,
      text: 'Speaking…',
      action: (
        <Button size="sm" variant="ghost" icon={<Stop size={14} />} onClick={stopSpeaking}>
          Stop
        </Button>
      ),
    }
  const sending = [...items].reverse().find((x) => x.state === 'sending' && x.slow)
  if (sending) return { key: `s${sending.clientId}`, icon: <span className="spinner" aria-hidden="true" />, text: 'Sending…' }
  const queued = [...items].reverse().find((x) => x.state === 'queued')
  if (queued) return { key: `q${queued.clientId}`, icon: <Waiting size={16} />, text: queued.msg.interrupt ? 'Stopping the current step…' : both('Queued. Alexa will read this after the current step.', 'Queued for after this step.') }
  const seen = [...items].reverse().find((x) => x.state === 'seen')
  if (seen) return { key: `v${seen.clientId}`, tone: 'ok', icon: <Check size={16} />, text: 'Seen' }
  return null
}

const keep = (o: { items: Item[] }) => o.items

// Receipts is the composer's status line, above what you type: whether
// your last message went, and whether Alexa has read it yet.
export function Receipts(p: ReceiptProps) {
  const items = useOutbox(keep, shallow)
  const speaking = useSpeaking()
  const row = receiptFor(items, p, speaking)
  return row ? <ReceiptLine row={row} /> : null
}

export function ReceiptLine({ row }: { row: Row }) {
  return (
    <div key={row.key} className={cx('receipt', row.tone)} role={row.tone === 'bad' ? 'alert' : 'status'}>
      <span className="rc-ic">{row.icon}</span>
      <span className="rc-text">{row.text}</span>
      {row.action}
      {row.close && (
        <button type="button" className="rc-x" aria-label="Dismiss" onClick={row.close}>
          <Close size={14} />
        </button>
      )}
    </div>
  )
}
