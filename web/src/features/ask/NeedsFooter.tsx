import type { ReactNode } from 'react'
import { stop } from '../../state/actions'
import { Button } from '../../ui/Button'
import { Stop } from '../../ui/icons'

type Props = {
  hint?: ReactNode
  // onSkip answers "carry on without it" as a visible message; the card
  // stays answerable from Needs you. Without it there's no Skip.
  onSkip?: () => void
  off?: boolean
}

// NeedsFooter is the quiet last row of a needs-you card: a hint, then Skip
// and Stop. They're two different things: Skip lets Alexa carry on without
// the answer, Stop ends the task (FE-14). The buttons say so in their own
// words, since a title never shows on a touch screen.
export function NeedsFooter({ hint, onSkip, off }: Props) {
  return (
    <div className="needs-foot">
      {hint && <p className="needs-hint">{hint}</p>}
      <div className="needs-end">
        {onSkip && (
          <Button variant="ghost" size="sm" disabled={off} title="Alexa carries on without it. You can still answer from Needs you." onClick={onSkip}>
            Skip for now
          </Button>
        )}
        <Button variant="ghost" size="sm" className="needs-stop" icon={<Stop size={14} />} title="Stop this task" onClick={stop}>
          Stop task
        </Button>
      </div>
    </div>
  )
}
