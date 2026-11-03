import { createContext, useContext } from 'react'
import type { Block, Placement } from '../../api/blocks'
import type { Entry } from '../../api/types'

// Where a tapped reply came from: the entry, the block's index in it and
// the card's index in the block. The server reads it to tell Alexa what was
// tapped ([Tapped "Choose this" on: Sony WH-1000XM6], Appendix A.4).
export type Tap = { entryId: number; block: number; item: number }

// BlockCtx is what every renderer in one BlockList shares: where it shows,
// whether its replies still work, and what to do when one is tapped.
export type BlockCtx = {
  placement: Placement
  // frozen: a past exchange, or one already answered. Replies are
  // disabled; links, downloads and the calendar still work.
  frozen: boolean
  // answer is what the user said next, when there's a next message.
  answer?: string
  // sent is the reply tapped here a moment ago, before its entry arrives.
  sent?: string
  entryId?: number
  zone?: string // the user's time zone, for instants
  reply: (text: string, at: { block: number; item: number }) => void
  // expand opens the entry's blocks in the panel.
  expand: () => void
}

export const Ctx = createContext<BlockCtx>({ placement: 'inline', frozen: true, reply: () => {}, expand: () => {} })
export const useBlocks = () => useContext(Ctx)

// BlockIndex is the block's place in its list, for taps.
export const IndexCtx = createContext(0)
export const useBlockIndex = () => useContext(IndexCtx)

// answerEntryAfter is the first message the user sent after the entry:
// what they answered, by a tap or in their own words.
export function answerEntryAfter(feed: Entry[], entryId?: number): Entry | undefined {
  if (entryId === undefined) return undefined
  const i = feed.findIndex((e) => e.id === entryId)
  if (i < 0) return undefined
  for (let j = i + 1; j < feed.length; j++) {
    if (feed[j].kind !== 'user') continue
    // A message that set Alexa to work afresh is a new request, not an
    // answer to these blocks.
    return feed[j].data?.start ? undefined : feed[j]
  }
  return undefined
}

// answerAfter is the text of that message.
export const answerAfter = (feed: Entry[], entryId?: number) => answerEntryAfter(feed, entryId)?.text

// chosenLabel is the label of the reply button whose text the user's answer
// is, when the answer was one of the cards' replies.
export function chosenLabel(blocks: Block[], answer: string): string | undefined {
  const a = answer.trim()
  for (const b of blocks) {
    if (b?.type !== 'cards' || !Array.isArray(b.items)) continue
    for (const c of b.items) if (c?.action?.do === 'reply' && c.action.text?.trim() === a) return c.action.label || a
  }
  return undefined
}
