import { useEffect, useState } from 'react'
import type { BlockOf } from '../../../api/blocks'
import { Pill, type Tone } from '../../../ui/Pill'
import { Check, Copy } from '../../../ui/icons'
import { Money } from '../parts/Money'
import './receipt.css'

type B = BlockOf<'receipt'>

const status: Record<B['status'], { word: string; tone: Tone }> = {
  draft: { word: 'Draft', tone: 'neutral' },
  pending: { word: 'Pending', tone: 'neutral' },
  confirmed: { word: 'Confirmed', tone: 'ok' },
  cancelled: { word: 'Cancelled', tone: 'neutral' },
  failed: { word: 'Failed', tone: 'bad' },
}

// Receipt is the moment something is bought, booked or sent, treated like
// a ticket: who and what, the lines, a perforation, then the total large
// and the reference you can copy. A draft is what Alexa is about to do; a
// confirmed one is what she did.
export function Receipt({ block: b }: { block: B }) {
  const s = status[b.status] ?? status.draft
  const title = b.title || b.merchant
  return (
    <div className={`b-receipt st-${b.status}`}>
      <div className="b-rc-top">
        <div className="b-rc-head">
          <div className="b-rc-who">
            {title && <b>{title}</b>}
            {b.title && b.merchant && <span>{b.merchant}</span>}
          </div>
          <Pill tone={s.tone} dot={b.status === 'pending'}>
            {b.status === 'confirmed' && <Check size={12} />}
            {s.word}
          </Pill>
        </div>
        {b.lines.length > 0 && (
          <dl className="b-rc-lines">
            {b.lines.map((l, i) => (
              <div key={i}>
                <dt>{l.label}</dt>
                <dd>{l.value}</dd>
              </div>
            ))}
          </dl>
        )}
      </div>
      <div className="b-rc-stub">
        <div className="b-rc-total">
          <span>{b.status === 'draft' ? 'Total to pay' : 'Total'}</span>
          <Money amount={b.total.amount} currency={b.total.currency} cents />
        </div>
        {b.ref && <Ref value={b.ref} />}
      </div>
    </div>
  )
}

// Ref is the booking or order reference, with a copy button.
function Ref({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return
    const t = setTimeout(() => setCopied(false), 1600)
    return () => clearTimeout(t)
  }, [copied])
  const copy = () => {
    navigator.clipboard?.writeText(value).then(
      () => setCopied(true),
      () => {},
    )
  }
  return (
    <div className="b-rc-ref">
      <span className="label">Reference</span>
      <code>{value}</code>
      <button type="button" className="b-copy" onClick={copy} aria-label={copied ? 'Copied' : `Copy reference ${value}`}>
        {copied ? <Check size={14} /> : <Copy size={14} />}
        <span aria-live="polite">{copied ? 'Copied' : 'Copy'}</span>
      </button>
    </div>
  )
}
