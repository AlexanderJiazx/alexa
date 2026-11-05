import { useEffect, useMemo, useState } from 'react'
import type { Block, Placement } from '../../api/blocks'
import { reply, show } from '../../state/actions'
import { useUser } from '../../state/selectors'
import { useApp } from '../../state/store'
import { Boundary, Fallback } from './Boundary'
import { answerEntryAfter, chosenLabel, Ctx, IndexCtx, type BlockCtx, type Tap } from './context'
import { AnswerNote } from './parts/AnswerNote'
import { renderers } from './registry'
import { summarize } from './summary'
import { known, valid } from './valid'
import './blocks.css'

export type BlockListProps = {
  blocks?: Block[]
  placement: Placement
  // frozen is a past exchange: replies are disabled and show what was
  // answered.
  frozen?: boolean
  // The entry the blocks belong to, for Expand (Surface {kind:"entry", id}).
  entryId?: number
  // echo says what was answered under frozen replies. Only for a view that
  // doesn't show the answer as the next message (the expanded entry):
  // anywhere else the user's own message, just below, already says it.
  echo?: boolean
  // The gallery and tests stand in for the real reply and Expand, which
  // reach the server; so does a confirm card, which sends a reply with its
  // own tap. One that sends resolves with whether the reply went.
  onReply?: (text: string, tap: Tap) => void | Promise<boolean>
  onExpand?: () => void
}

// How long a tapped reply waits for its message before the buttons work
// again, should the send never settle.
const sentFor = 10e3

// replyFailed resolves true when a reply's send says it couldn't go. A
// stand-in that sends nothing never fails.
export const replyFailed = async (going: void | Promise<boolean>) => (await going) === false

// BlockList renders the generative UI blocks attached to a say, an ask or
// a report (Appendix A), natively and theme-aware. Each block is a group
// named by its summary; a block that doesn't check out, or throws while
// drawing, shows its summary instead; a type this client doesn't know is
// skipped. Once the user has answered (or it's history), replies are
// frozen and the chosen one says so.
export function BlockList({ blocks, placement, frozen, entryId, echo, onReply, onExpand }: BlockListProps) {
  const list = useMemo(() => (Array.isArray(blocks) ? blocks.filter(known) : []), [blocks])
  const zone = useUser()?.timezone || undefined
  const answer = useApp((s) => answerEntryAfter(s.data.feed, entryId))
  const answered = answer?.text
  const [sent, setSent] = useState<string>()
  useEffect(() => {
    if (!sent) return
    const t = setTimeout(() => setSent(undefined), sentFor)
    return () => clearTimeout(t)
  }, [sent])

  const ctx = useMemo<BlockCtx>(
    () => ({
      placement,
      frozen: !!frozen || answered !== undefined,
      answer: answered,
      sent: answered === undefined ? sent : undefined,
      entryId,
      zone,
      reply: (text, at) => {
        setSent(text)
        const going = onReply ? onReply(text, { entryId: entryId ?? 0, ...at }) : reply(text, entryId === undefined ? undefined : { entryId, ...at })
        // A reply that couldn't go frees the buttons at once, whoever sent
        // it; the composer's receipt says why and offers Retry.
        void replyFailed(going).then((failed) => {
          if (failed) setSent((s) => (s === text ? undefined : s))
        })
      },
      expand: () => {
        if (onExpand) onExpand()
        else if (entryId !== undefined) show({ kind: 'entry', id: String(entryId) })
      },
    }),
    [placement, frozen, answered, sent, entryId, zone, onReply, onExpand],
  )

  if (!list.length) return null
  const replies = list.some(hasReply)
  return (
    <Ctx.Provider value={ctx}>
      <div className="blocks" data-place={placement}>
        {list.map((b, i) => (
          <IndexCtx.Provider key={i} value={i}>
            <BlockView block={b} />
          </IndexCtx.Provider>
        ))}
        {echo && replies && answer && <AnswerNote chose={chosenLabel(list, answer.text)} said={answer.text} at={answer.createdAt} />}
      </div>
    </Ctx.Provider>
  )
}

// BlockView is one block: checked, then drawn inside its own boundary.
function BlockView({ block }: { block: Block }) {
  const R = renderers[block.type]
  return (
    <div className="b" data-type={block.type} role="group" aria-label={summarize(block) || undefined}>
      {valid(block) ? (
        <Boundary block={block}>
          <R block={block} />
        </Boundary>
      ) : (
        <Fallback block={block} />
      )}
    </div>
  )
}

const hasReply = (b: Block) => b.type === 'cards' && Array.isArray(b.items) && b.items.some((c) => c?.action?.do === 'reply')
