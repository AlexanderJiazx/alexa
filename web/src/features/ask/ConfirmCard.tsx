import { useMemo } from 'react'
import type { Tap } from '../../api/blocks'
import { moneyParts } from '../../lib/format'
import { reply as sendReply } from '../../state/actions'
import { Button } from '../../ui/Button'
import { Favicon, hostName } from '../../ui/Favicon'
import { Check } from '../../ui/icons'
import { BlockList } from '../blocks/BlockList'
import { confirmOf, type Ask, type Choice, type Receipt } from './model'
import type { Reply } from './reply'

type Props = {
  ask: Ask
  reply: Reply
  compact?: boolean
  answer: (text: string) => void
}

const looksLikeHost = (s: string) => /^[a-z0-9-]+(\.[a-z0-9-]+)+$/i.test(s.trim())

// Total is the receipt's total, large and tabular, with the currency symbol
// a step smaller so the figure leads.
export function Total({ receipt }: { receipt: Receipt }) {
  const t = receipt.total
  const p = moneyParts(t.amount, t.currency, undefined, true)
  return (
    <p className="cf-total">
      <span className="cf-total-label">Total</span>
      <span className="cf-total-value num" aria-label={`${p.symbolFirst ? p.symbol : ''}${p.value}${p.symbolFirst ? '' : ` ${p.symbol}`}`}>
        {p.symbolFirst && <span className="cf-cur">{p.symbol}</span>}
        {p.value}
        {!p.symbolFirst && <span className="cf-cur"> {p.symbol}</span>}
      </span>
    </p>
  )
}

// ConfirmSummary is what will happen, from the draft receipt: who it's
// with, each line, and the total. The confirm card draws it itself (rather
// than through BlockList) so the total sits by the buttons once, and is
// always the receipt's own.
function ConfirmSummary({ receipt }: { receipt: Receipt }) {
  const m = receipt.merchant?.trim()
  return (
    <div className="cf-summary">
      {m && (
        <p className="cf-merchant">
          {looksLikeHost(m) && <Favicon host={m} size={20} />}
          <span>{looksLikeHost(m) ? hostName(m) : m}</span>
        </p>
      )}
      {receipt.lines.length > 0 && (
        <dl className="cf-lines">
          {receipt.lines.map((l, i) => (
            <div key={i} className="cf-line">
              <dt>{l.label}</dt>
              <dd className="num">{l.value}</dd>
            </div>
          ))}
        </dl>
      )}
      <Total receipt={receipt} />
    </div>
  )
}

// ConfirmCard is an ask that confirms something consequential (an order, a
// booking, a message to send): what will happen, then Confirm, Change or
// Cancel. Each sends a visible message, in the model's own words where it
// offered them. It has no keyboard shortcut and takes no focus when it
// arrives, so nothing is confirmed by a stray key.
export function ConfirmCard({ ask, reply, compact, answer }: Props) {
  const c = useMemo(() => confirmOf(ask), [ask])
  // The receipt is drawn here, not by BlockList, so a card tapped below it
  // is one place further on in the ask's blocks than in BlockList's: the
  // tap goes out with its index in the ask, which is what the server reads.
  // BlockList gets back whether it went, to free its buttons if not.
  const onReply = useMemo(() => {
    if (c.restAt.every((at, i) => at === i)) return undefined
    const id = ask.id
    return (text: string, tap: Tap) => sendReply(text, id === undefined ? undefined : { entryId: id, block: c.restAt[tap.block] ?? tap.block, item: tap.item })
  }, [c, ask.id])
  const off = reply.busy || reply.sent
  const btn = (ch: Choice, variant: 'primary' | 'secondary' | 'ghost') => {
    const chosen = off && reply.which === ch.text
    return (
      <Button variant={variant} size={compact ? 'sm' : 'md'} className={`cf-${variant}`} disabled={off && !chosen} aria-pressed={chosen || undefined} onClick={() => !off && answer(ch.text)}>
        {chosen && (reply.busy ? <span className="spinner" aria-hidden="true" /> : <Check size={16} />)}
        {ch.label}
      </Button>
    )
  }
  return (
    <>
      {c.receipt && <ConfirmSummary receipt={c.receipt} />}
      <BlockList blocks={c.rest} placement="inline" entryId={ask.id} onReply={onReply} />
      <div className="cf-actions">
        {btn(c.confirm, 'primary')}
        {btn(c.change, 'secondary')}
        {btn(c.cancel, 'ghost')}
      </div>
      {!compact && (
        <p className="cf-note">Nothing happens until you confirm.</p>
      )}
    </>
  )
}
