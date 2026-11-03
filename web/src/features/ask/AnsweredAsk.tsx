import type { Entry } from '../../api/types'
import { cx } from '../../ui/cx'
import { Check } from '../../ui/icons'
import { BlockList } from '../blocks/BlockList'
import { AnswerNote } from '../blocks/parts/AnswerNote'
import { Speaker } from '../conversation/entries/Speaker'
import { chosenIn, type Answer } from './answer'
import './ask.css'

type Props = {
  ask: Entry
  // answer is how it was answered: null when it wasn't, undefined when
  // whoever draws it doesn't know (an entry on its own).
  answer?: Answer | null
}

// AnsweredAsk is one of Alexa's questions once it's no longer waiting, the
// same on the main screen and in Reflect (IX-F13): what it asked, the
// choices it offered with the ones picked ticked in solid ink, and "You
// chose …", or that it went unanswered. Nothing here can be answered any
// more. An answer that wasn't one of the choices (a form, a tapped card,
// your own words) follows as your message, so it isn't said here as well.
export function AnsweredAsk({ ask, answer }: Props) {
  const options = ask.data?.options ?? []
  const chosen = chosenIn(answer)
  return (
    <div className="ask-past">
      <div className="msg agent">
        <p data-ask-q={ask.id}>
          <Speaker who="alexa" />
          {ask.text}
        </p>
      </div>
      <BlockList blocks={ask.data?.blocks} placement="inline" frozen entryId={ask.id} />
      {options.length > 0 && !ask.data?.form && (
        <ul className="ask-was" aria-label="The choices offered">
          {options.map((o, i) => {
            const on = chosen.includes(o.trim())
            return (
              <li key={`${i}:${o}`} className={cx(on && 'chosen')} aria-current={on || undefined}>
                {on && <Check size={14} />}
                {o}
              </li>
            )
          })}
        </ul>
      )}
      {answer && chosen.length > 0 ? (
        <AnswerNote chose={chosen.join(', ')} at={answer.entry.createdAt} />
      ) : (
        answer === null && <p className="ask-unanswered">{ask.status === 'open' ? 'Still waiting on you' : 'Not answered'}</p>
      )}
    </div>
  )
}
