import { useEffect, useRef, type KeyboardEvent } from 'react'
import { composerEmpty, composerFocused } from '../../composer/focus'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { moveIndex } from '../../ui/focus'
import { Check } from '../../ui/icons'
import { Kbd } from '../../ui/Kbd'
import { finePointer, useAskKeys } from './keys'
import { optionLayout, type Ask } from './model'
import type { Reply } from './reply'

type Props = {
  ask: Ask
  reply: Reply
  // keys is true for the one card that answers 1 to 4 (not the dock's or
  // the caption's copy).
  keys: boolean
  compact?: boolean
  // late is a question skipped earlier: it takes keys only while focused.
  late?: boolean
  answer: (text: string) => void
}

// OptionList is a single choice: pills when the options are short, full
// rows when any is long. Every option looks the same until one is chosen,
// which turns solid ink, as a choice does everywhere else. The recommended
// option is ringed in ink and says so in words, so it never reads as
// already picked; with none recommended they're all equal, so Alexa never
// seems to prefer the first (T-L6).
export function OptionList({ ask, reply, keys, compact, late, answer }: Props) {
  const box = useRef<HTMLDivElement>(null)
  const rows = !compact && optionLayout(ask.options) === 'rows'
  const off = reply.busy || reply.sent
  const pick = (i: number) => {
    if (!off) answer(ask.options[i])
  }
  useAskKeys(keys && !off, { kind: 'choice', count: ask.options.length, recommended: ask.recommended, picked: 0 }, (k) => k.do === 'pick' && pick(k.index), { late, box })

  // When a question arrives while the user isn't typing, focus goes to it,
  // so the number keys and Enter answer at once. It goes to the group, not
  // to an option: a ring on the first would look like a preference. The
  // arrows then move between the options.
  useEffect(() => {
    if (!keys || !finePointer()) return
    const a = document.activeElement
    if (a && a !== document.body && !(composerFocused() && composerEmpty())) return
    box.current?.focus({ preventScroll: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keys, ask.id])

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const btns = Array.from(box.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])
    const at = btns.indexOf(document.activeElement as HTMLButtonElement)
    // From the group itself the first arrow lands on the recommended
    // option, or on the first one either way.
    const i = at < 0 && /^Arrow/.test(e.key) ? (ask.recommended ?? 0) : moveIndex(e.key, at, btns.length)
    if (i === null || !btns[i]) return
    e.preventDefault()
    btns[i].focus()
  }

  return (
    <div ref={box} className={cx('ask-opts', rows ? 'rows' : 'pills')} role="group" aria-label="Choices" tabIndex={-1} onKeyDown={onKeyDown}>
      {ask.options.map((o, i) => {
        const chosen = off && reply.which === o
        return (
          <Button
            key={`${i}:${o}`}
            variant="secondary"
            size={compact ? 'sm' : 'md'}
            block={rows}
            className={cx('ask-opt', i === ask.recommended && 'rec', chosen && 'chosen')}
            disabled={off && !chosen}
            aria-busy={(reply.busy && chosen) || undefined}
            aria-pressed={chosen || undefined}
            aria-keyshortcuts={keys ? String(i + 1) : undefined}
            onClick={() => pick(i)}
          >
            {keys && <Kbd>{i + 1}</Kbd>}
            <span className="ask-opt-label">{o}</span>
            {i === ask.recommended && !chosen && <span className="ask-rec">Recommended</span>}
            {chosen && (reply.busy ? <span className="spinner" aria-hidden="true" /> : <Check size={16} />)}
          </Button>
        )
      })}
    </div>
  )
}
