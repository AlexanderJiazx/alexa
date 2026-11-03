import { useRef, useState } from 'react'
import { useNeeds } from '../../../state/selectors'
import { AnsweredAsk } from '../../ask/AnsweredAsk'
import { AskCard } from '../../ask/AskCard'
import { Later } from '../../ask/icons'
import { useCardPresence, useIsSkipped } from '../../ask/presence'
import type { EntryProps } from '../registry'

// AskEntry is Alexa's question. While it waits it can be answered on the
// main screen; a question skipped for now can still be answered later; in
// Reflect, and once answered, it shows as it was asked, with the choices it
// offered and the one picked (AnsweredAsk, as Reflect draws it).
export function AskEntry({ entry: e, mode, answer }: EntryProps) {
  const skipped = useIsSkipped(e.id) && mode === 'now'
  // While Alexa waits on the user for something else, that comes first, as
  // in the dock: a skipped question is answered after it.
  const waiting = !!useNeeds()
  const [open, setOpen] = useState(false)
  if (e.status === 'open' && mode === 'now') return <AskCard entry={e} />
  if (skipped && open) return <AskCard entry={e} late />
  return (
    <>
      <AnsweredAsk ask={e} answer={answer} />
      {skipped && <Skipped id={e.id} onOpen={waiting ? undefined : () => setOpen(true)} />}
    </>
  )
}

// Skipped is the line under a question that was skipped for now, with
// Answer now unless something else waits on the user. While it's on screen
// the dock leaves the question to it.
function Skipped({ id, onOpen }: { id: number; onOpen?: () => void }) {
  const ref = useRef<HTMLParagraphElement>(null)
  useCardPresence(id, ref)
  return (
    <p ref={ref} className="ask-later">
      <Later size={14} />
      <span>You skipped this.</span>
      {onOpen && (
        <button type="button" className="linkish" onClick={onOpen}>
          Answer now
        </button>
      )}
    </p>
  )
}
