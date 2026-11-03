import { useEffect, useState } from 'react'
import { handBack, takeOver } from '../../state/actions'
import { useExchange } from '../../state/exchange'
import { useControl, useNeeds, useSurface } from '../../state/selectors'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { IconButton } from '../../ui/IconButton'
import { Chevron, Close } from '../../ui/icons'
import { Mark } from '../../ui/Mark'
import { AskCard } from './AskCard'
import { Later } from './icons'
import { LoginForm } from './LoginForm'
import { revealCard, unskip, useCardShown, useSkipped } from './presence'
import './ask.css'

// NeedsDock is the one card for whatever Alexa needs from the user (an
// answer, a sign-in, a take-over), docked above the composer when the card
// in the exchange is out of sight: scrolled away, or not in the exchange at
// all. A question that was skipped stays here too, to answer later. Its
// button brings the card back into view, or opens it right here.
export function NeedsDock() {
  const needs = useNeeds()
  const control = useControl()
  const surface = useSurface()
  const exchange = useExchange()
  const skipped = useSkipped()
  const [open, setOpen] = useState(false)
  // A skipped question shows while its exchange is on screen.
  const late = needs ? undefined : [...skipped].reverse().find((s) => exchange.some((e) => e.id === s.id))
  const id = needs ? needs.entryId : late?.id
  const shown = useCardShown(id)
  const key = needs ? `n${needs.kind}${needs.entryId ?? ''}` : late ? `s${late.id}` : ''
  useEffect(() => setOpen(false), [key])

  if ((!needs && !late) || (shown && !open)) return null
  // The browser panel's own head offers Take over and Hand back; the dock
  // doesn't say it twice.
  if (needs?.kind === 'takeover' && surface.kind === 'browser') return null

  const kind = needs?.kind ?? 'question'
  const label = late ? 'You skipped this' : kind === 'login' ? `Sign in to ${needs?.site || 'a site'}` : kind === 'takeover' ? 'Your turn in the browser' : 'Needs you'
  const text = late ? late.ask.question : (needs?.message ?? '')
  const show = () => {
    if (!late && kind !== 'takeover' && revealCard(id)) return
    setOpen((o) => !o)
  }

  let action
  if (kind === 'takeover' && !late)
    action = control ? (
      <Button variant="primary" size="sm" onClick={() => handBack()}>
        Hand back
      </Button>
    ) : (
      <Button variant="primary" size="sm" onClick={takeOver}>
        Take over
      </Button>
    )
  else
    action = (
      <Button variant={open ? 'secondary' : 'primary'} size="sm" aria-expanded={open} onClick={show}>
        {open ? 'Hide' : kind === 'login' ? 'Sign in' : 'Answer'}
      </Button>
    )

  return (
    <aside className={cx('needs-dock', open && 'open')} aria-label="Alexa needs you" key={key}>
      {open && (
        <div className="dock-body">
          {late ? (
            <AskCard ask={late.ask} compact late />
          ) : kind === 'login' && needs ? (
            <LoginForm dock site={needs.site || ''} username={needs.username} reason={needs.message} control={control} onTakeOver={takeOver} onHandBack={() => handBack()} />
          ) : needs ? (
            <AskCard needs={needs} compact />
          ) : null}
        </div>
      )}
      <div className="dock-row">
        {late ? <Later size={16} /> : <Mark state="needs" size="sm" />}
        <button type="button" className="dock-text" onClick={show}>
          <span className="dock-label">{label}</span>
          <span className="dock-msg">{text}</span>
        </button>
        {open ? (
          <IconButton label="Close" size="sm" tone="muted" onClick={() => setOpen(false)}>
            <Chevron size={18} />
          </IconButton>
        ) : (
          action
        )}
        {late && !open && (
          <IconButton label="Forget this question" size="sm" tone="muted" onClick={() => unskip(late.id)}>
            <Close size={16} />
          </IconButton>
        )}
      </div>
    </aside>
  )
}
