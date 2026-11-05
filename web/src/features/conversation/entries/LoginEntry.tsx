import { useRef } from 'react'
import { handBack, takeOver } from '../../../state/actions'
import { useControl } from '../../../state/selectors'
import { LoginForm } from '../../ask/LoginForm'
import { useCardPresence } from '../../ask/presence'
import type { EntryProps } from '../registry'
import { Speaker } from './Speaker'

// LoginEntry is Alexa asking to sign in to a site.
export function LoginEntry({ entry: e, mode }: EntryProps) {
  if (!(e.status === 'open' && mode === 'now'))
    return (
      <div className="msg agent muted">
        <p>
          <Speaker who="alexa" />
          {e.text}
        </p>
      </div>
    )
  return <LoginCard id={e.id} site={e.data?.site || ''} username={e.data?.username} reason={e.text} />
}

function LoginCard({ id, site, username, reason }: { id: number; site: string; username?: string; reason: string }) {
  const control = useControl()
  const ref = useRef<HTMLDivElement>(null)
  useCardPresence(id, ref)
  return (
    <div ref={ref} className="ask-slot">
      <LoginForm site={site} username={username} reason={reason} control={control} onTakeOver={takeOver} onHandBack={() => handBack()} />
    </div>
  )
}
