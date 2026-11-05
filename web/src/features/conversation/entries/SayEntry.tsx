import { BlockList } from '../../blocks/BlockList'
import { Sources } from '../../blocks/Sources'
import { linkify } from '../linkify'
import type { EntryProps } from '../registry'
import { Speaker } from './Speaker'

// SayEntry is what Alexa said: its only way of talking to the user, with
// the blocks and sources it attached.
export function SayEntry({ entry: e, mode }: EntryProps) {
  return (
    <>
      <div className="msg agent">
        <p>
          <Speaker who="alexa" />
          {linkify(e.text)}
        </p>
      </div>
      <BlockList blocks={e.data?.blocks} placement="inline" frozen={mode === 'history'} entryId={e.id} />
      <Sources sources={e.data?.sources} />
    </>
  )
}
