import type { Entry } from '../api/types'
import { AnsweredAsk } from '../features/ask/AnsweredAsk'
import type { Answer } from './exchanges'

// PastAsk is one of Alexa's questions as Reflect shows it (IX-F13), drawn
// as the main screen draws an answered one (AnsweredAsk): what it asked,
// the choices it offered and which ones you picked, or that it went
// unanswered. An answer that wasn't one of the choices follows as your
// message (History).
export function PastAsk({ ask, answer }: { ask: Entry; answer: Answer | null }) {
  return <AnsweredAsk ask={ask} answer={answer} />
}
