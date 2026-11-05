import { useState } from 'react'
import { handBack, stop, takeOver } from '../../state/actions'
import { latestSaid, useExchange } from '../../state/exchange'
import { useSession } from '../../state/selectors'
import { AskCard } from '../ask/AskCard'
import { Button } from '../../ui/Button'
import { IconButton } from '../../ui/IconButton'
import { Stop } from '../../ui/icons'
import { Mark } from '../../ui/Mark'
import { LoginForm } from '../ask/LoginForm'
import './caption.css'

// Caption keeps the conversation in reach while a panel fills a small
// screen (the sheet at its full height): Alexa's question, what it's doing,
// or the last thing it said, as one line just above the composer.
export function Caption() {
  const { status, needs, control, activity, browser } = useSession((s) => ({ status: s.status, needs: s.needs, control: s.control, activity: s.activity, browser: s.surface.kind === 'browser' }))
  const latest = latestSaid(useExchange())
  const nd = status === 'needs' ? needs : null
  // The same card as in the exchange, compact: the recommended answer,
  // multi-select, a form or a confirm all work here too.
  if (nd?.kind === 'question')
    return (
      <div className="caption ask">
        <AskCard needs={nd} compact />
      </div>
    )
  if (nd?.kind === 'login') return <LoginForm compact site={nd.site || ''} username={nd.username} reason={nd.message} control={control} onTakeOver={takeOver} onHandBack={() => handBack()} onStop={stop} />
  // Over the browser, its head has Take over and Hand back already.
  if (nd?.kind === 'takeover')
    return (
      <div className="caption">
        <Mark state="needs" size="sm" />
        <p>{nd.message}</p>
        {!browser && (
          <Button size="sm" variant="primary" onClick={control ? () => handBack() : takeOver}>
            {control ? 'Hand back' : 'Take over'}
          </Button>
        )}
      </div>
    )
  if (status === 'working')
    return (
      <div className="caption">
        <Mark state={control ? 'waiting' : 'working'} size="sm" />
        <p>{control ? 'You’re in control' : activity || 'Working on it'}</p>
        <IconButton label="Stop" size="sm" tone="muted" onClick={stop}>
          <Stop size={16} />
        </IconButton>
      </div>
    )
  if (latest) return <Said key={latest.id} text={latest.text} />
  return null
}

// Said is Alexa's last message, clamped to two lines until tapped.
function Said({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  return (
    <button type="button" className={`caption said${open ? ' open' : ''}`} aria-expanded={open} onClick={() => setOpen(!open)}>
      <Mark size="sm" />
      <p>{text}</p>
    </button>
  )
}
