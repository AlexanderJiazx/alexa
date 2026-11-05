import { useRef } from 'react'
import { handBack, takeOver } from '../../../state/actions'
import { useControl } from '../../../state/selectors'
import { Button } from '../../../ui/Button'
import { HandBack, Pointer } from '../../../ui/icons'
import { Mark } from '../../../ui/Mark'
import { NeedsFooter } from '../../ask/NeedsFooter'
import { useCardPresence } from '../../ask/presence'
import { useReply } from '../../ask/reply'
import type { EntryProps } from '../registry'
import { Speaker } from './Speaker'

const skipText = 'Skip this step for now. Carry on without it.'

// TakeoverEntry is Alexa handing the browser to the user: a CAPTCHA, a
// payment, something only they should do.
export function TakeoverEntry({ entry: e, mode }: EntryProps) {
  if (!(e.status === 'open' && mode === 'now'))
    return (
      <div className="msg agent muted">
        <p>
          <Speaker who="alexa" />
          {e.text}
        </p>
      </div>
    )
  return <TakeoverCard id={e.id} text={e.text} />
}

function TakeoverCard({ id, text }: { id: number; text: string }) {
  const control = useControl()
  const ref = useRef<HTMLElement>(null)
  useCardPresence(id, ref)
  const skip = useReply()
  return (
    <section ref={ref} className="ask ask-takeover" aria-label="Alexa needs you in the browser">
      <div className="eyebrow">
        <Mark state={control ? 'waiting' : 'needs'} size="sm" />
        <span>{control ? 'You’re in control' : 'Your turn in the browser'}</span>
      </div>
      <p className="q">{text}</p>
      <div className="opts">
        {control ? (
          <Button variant="primary" icon={<HandBack size={16} />} onClick={() => handBack()}>
            Hand back
          </Button>
        ) : (
          <Button variant="primary" icon={<Pointer size={16} />} onClick={takeOver}>
            Take over
          </Button>
        )}
      </div>
      {skip.error && (
        <p className="ask-err" role="alert">
          <span>Couldn’t send</span>
          <span aria-hidden="true">·</span>
          <button type="button" className="linkish" onClick={skip.retry}>
            Retry
          </button>
        </p>
      )}
      <NeedsFooter hint={control ? 'Hand back when you’re done, and Alexa carries on.' : 'Alexa waits while you’re in the browser.'} onSkip={control ? undefined : () => skip.send({ text: skipText })} off={skip.busy || skip.sent} />
    </section>
  )
}
