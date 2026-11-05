import { useState } from 'react'
import { Button } from '../../ui/Button'
import { Chip } from '../../ui/Chip'
import { Check } from '../../ui/icons'
import { Kbd } from '../../ui/Kbd'
import { useAskKeys } from './keys'
import { multiText, type Ask } from './model'
import type { Reply } from './reply'
import { useSentFocus } from './sentFocus'

type Props = {
  ask: Ask
  reply: Reply
  keys: boolean
  compact?: boolean
  // late is a question skipped earlier: it takes keys only while focused.
  late?: boolean
  answer: (text: string) => void
}

// MultiChips is a multi-select: toggle the options, then Done sends them in
// the order offered, "Amazon, B&H".
export function MultiChips({ ask, reply, keys, compact, late, answer }: Props) {
  const [picked, setPicked] = useState<Set<number>>(() => new Set())
  const off = reply.busy || reply.sent
  const { box, sent, before } = useSentFocus<HTMLDivElement>(off)
  const toggle = (i: number) => {
    if (off) return
    setPicked((p) => {
      const n = new Set(p)
      if (n.has(i)) n.delete(i)
      else n.add(i)
      return n
    })
  }
  const done = () => {
    if (off || !picked.size) return
    before()
    answer(multiText(ask.options, picked))
  }
  useAskKeys(
    keys && !off,
    { kind: 'multi', count: ask.options.length, picked: picked.size },
    (k) => {
      if (k.do === 'toggle') toggle(k.index)
      if (k.do === 'done') done()
    },
    { late, box },
  )
  // Once Done is pressed, the picks give way to what was sent, as a
  // form's fields do.
  if (off)
    return (
      <p ref={sent} className="ask-sent" tabIndex={-1} aria-live="polite">
        {reply.busy ? <span className="spinner" aria-hidden="true" /> : <Check size={16} />}
        <span>{reply.which}</span>
      </p>
    )
  return (
    <div ref={box} className="ask-multi">
      <div className="ask-chips" role="group" aria-label="Choose any">
        {ask.options.map((o, i) => (
          <Chip key={`${i}:${o}`} selected={picked.has(i)} icon={picked.has(i) ? <Check size={14} /> : undefined} aria-keyshortcuts={keys ? String(i + 1) : undefined} onClick={() => toggle(i)}>
            {o}
            {keys && <Kbd>{i + 1}</Kbd>}
          </Chip>
        ))}
      </div>
      {/* Done turns primary once there's something to send, so an empty
          pick doesn't sit there as a heavy grey block. */}
      <Button variant={picked.size ? 'primary' : 'secondary'} size={compact ? 'sm' : 'md'} disabled={!picked.size} onClick={done}>
        {picked.size ? `Done · ${picked.size}` : 'Done'}
      </Button>
    </div>
  )
}
