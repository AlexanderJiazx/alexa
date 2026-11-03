import type { Entry, Needs } from '../../api/types'
import { show, takeOver } from '../../state/actions'
import { Button } from '../../ui/Button'
import { Mark } from '../../ui/Mark'
import { AskCard } from '../ask/AskCard'
import { Section } from './Section'

type Props = { needs: Needs; entry?: Entry; control: boolean }

// NeedsCard is what Alexa is waiting on, first on the board: its question
// (answered right here, as a visible message), or the browser it needs you
// in.
export function NeedsCard({ needs, entry, control }: Props) {
  // A question is the full ask card. When its entry isn't in the loaded
  // feed, the session's needs carries the same ask (a multi-select, a form,
  // a confirm card), so the card is drawn from that rather than as plain
  // buttons. Either card says "Needs you" itself.
  const question = needs.kind === 'question'
  return (
    <Section title="Needs you" className="home-needs" untitled>
      {question ? <AskCard entry={entry} needs={needs} /> : <Plain needs={needs} control={control} />}
    </Section>
  )
}

// Plain is the card for a take-over or a sign-in, which happen in the
// browser: the ask card's frame, eyebrow and question, with one button.
function Plain({ needs, control }: { needs: Needs; control: boolean }) {
  const browser = () => show({ kind: 'browser' })
  return (
    <section className="ask home-need" aria-label="Alexa needs you in the browser">
      <div className="eyebrow">
        <Mark state="needs" size="sm" />
        <span>Needs you</span>
      </div>
      <p className="q">{needs.message}</p>
      {needs.kind === 'takeover' && (
        <div className="row-btns">
          <Button
            size="sm"
            variant="primary"
            onClick={() => {
              browser()
              if (!control) takeOver()
            }}
          >
            Take over
          </Button>
        </div>
      )}
      {needs.kind === 'login' && (
        <div className="row-btns">
          <Button size="sm" variant="primary" onClick={browser}>
            {needs.site ? `Sign in to ${needs.site}` : 'Sign in'}
          </Button>
        </div>
      )}
    </section>
  )
}
