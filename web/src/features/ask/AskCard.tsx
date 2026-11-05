import { useMemo, useRef } from 'react'
import type { Entry, Needs } from '../../api/types'
import type { Outgoing } from '../../composer/outbox'
import { focusComposer } from '../../composer/focus'
import { cx } from '../../ui/cx'
import { Shield } from '../../ui/icons'
import { Mark } from '../../ui/Mark'
import { BlockList } from '../blocks/BlockList'
import { AskForm } from './AskForm'
import { ConfirmCard } from './ConfirmCard'
import { Later } from './icons'
import { finePointer } from './keys'
import { askFromEntry, askFromNeeds, askKind, confirmOf, lateText, skipText, type Ask } from './model'
import { MultiChips } from './MultiChips'
import { NeedsFooter } from './NeedsFooter'
import { OptionList } from './OptionList'
import { markSkipped, unskip, useCardPresence } from './presence'
import { useReply } from './reply'
import './ask.css'

type Props = {
  entry?: Entry
  // needs draws the ask from the session when there's no entry: the dock,
  // the caption under a panel, and the home board when the entry isn't
  // loaded.
  needs?: Needs
  // ask is an ask already worked out (a skipped one, kept in Needs you).
  ask?: Ask
  // compact is the smaller copy in the dock and the caption: no context
  // blocks, no keys, the same choices.
  compact?: boolean
  // late is a question that was skipped: an answer says which question it
  // answers, since the run has moved on.
  late?: boolean
}

// AskCard is Alexa's question while it waits: a single choice, a
// multi-select, a small form, a confirm card, or a question answered in
// the composer. Every answer is a visible message.
export function AskCard({ entry, needs, ask: given, compact, late }: Props) {
  const ask = useMemo(() => given ?? (entry ? askFromEntry(entry) : needs ? askFromNeeds(needs) : null), [given, entry, needs])
  if (!ask) return null
  return <AskView ask={ask} compact={compact} late={late} />
}

function hintFor(a: Ask, keys: boolean) {
  switch (askKind(a)) {
    case 'choice':
      return keys && finePointer() && a.options.length > 1 ? `Press 1–${a.options.length} to choose, or type an answer below.` : 'Pick one, or type an answer below.'
    case 'multi':
      return 'Pick any, then Done.'
    case 'form':
      return 'Or answer in your own words below.'
    case 'free':
      return (
        <>
          Type your answer below.{' '}
          <button type="button" className="linkish" onClick={focusComposer}>
            Answer
          </button>
        </>
      )
    default:
      return null
  }
}

function AskView({ ask, compact, late }: { ask: Ask; compact?: boolean; late?: boolean }) {
  const ref = useRef<HTMLElement>(null)
  // Only the exchange's own card counts for the dock.
  useCardPresence(ask.id, ref, !compact)
  const onSent = useMemo(() => (late && ask.id !== undefined ? () => unskip(ask.id as number) : undefined), [late, ask.id])
  const reply = useReply(onSent)
  const kind = askKind(ask)
  const keys = !compact
  const off = reply.busy || reply.sent

  // A late answer names its question's entry, so the server reads it
  // against that question's form, never the one open now (FE-1).
  const answer = (text: string, extra: Partial<Outgoing> = {}) => reply.send({ ...extra, text: late ? lateText(ask.question, text) : text, askEntry: late ? ask.id : undefined }, text)
  const skip = () =>
    reply.send({ text: skipText }, skipText).then((ok) => {
      if (ok) markSkipped(ask)
    })

  const eyebrow =
    kind === 'confirm' ? (
      <>
        <Shield size={16} />
        <span>{confirmOf(ask).eyebrow}</span>
      </>
    ) : late ? (
      <>
        <Later size={16} />
        <span>You skipped this. Answer any time.</span>
      </>
    ) : (
      <>
        <Mark state="needs" size="sm" />
        <span>Needs you</span>
      </>
    )

  return (
    <section ref={ref} className={cx('ask', `is-${kind}`, compact && 'compact', late && 'late')} data-ask={ask.id} aria-label={kind === 'confirm' ? 'Alexa wants you to confirm' : 'Alexa is asking'}>
      {!compact && <div className="eyebrow">{eyebrow}</div>}
      <p className="q">{ask.question}</p>
      {!compact && kind !== 'confirm' && <BlockList blocks={ask.blocks} placement="inline" entryId={ask.id} />}
      {kind === 'confirm' && <ConfirmCard ask={ask} reply={reply} compact={compact} answer={(t) => answer(t)} />}
      {kind === 'choice' && <OptionList ask={ask} reply={reply} keys={keys} compact={compact} late={late} answer={(t) => answer(t)} />}
      {kind === 'multi' && <MultiChips ask={ask} reply={reply} keys={keys} compact={compact} late={late} answer={(t) => answer(t, { via: 'multi' })} />}
      {kind === 'form' && ask.form && <AskForm form={ask.form} reply={reply} compact={compact} send={(t, values) => answer(t, { via: 'form', values })} />}
      {ask.dropped > 0 && !off && <p className="ask-dropped">Left out {ask.dropped === 1 ? 'a field' : `${ask.dropped} fields`} that looked private. Alexa never asks for passwords or codes here.</p>}
      {reply.error && (
        <p className="ask-err" role="alert">
          <span>Couldn’t send</span>
          <span aria-hidden="true">·</span>
          <button type="button" className="linkish" onClick={reply.retry}>
            Retry
          </button>
        </p>
      )}
      {/* Once answered, there's nothing to hint at and nothing to skip;
          Stop stays, since the run goes on. */}
      {kind !== 'confirm' && <NeedsFooter hint={compact || reply.sent ? null : hintFor(ask, keys)} onSkip={late || reply.sent ? undefined : skip} off={off} />}
    </section>
  )
}
