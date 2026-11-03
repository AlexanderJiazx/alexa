import type { EntryProps } from '../registry'
import { Speaker } from './Speaker'

// FallbackEntry shows an entry of a kind this client doesn't know as its
// text, so a newer server never breaks the conversation.
export function FallbackEntry({ entry: e }: EntryProps) {
  if (!e.text) return null
  return (
    <div className="msg agent">
      <p>
        <Speaker who="alexa" />
        {e.text}
      </p>
    </div>
  )
}
