import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import type { Tap } from '../../api/blocks'
import type { BlockListProps } from '../blocks/BlockList'
import { ConfirmCard } from './ConfirmCard'
import { receipt } from './fixtures'
import { askFromEntry } from './model'
import type { Reply } from './reply'

// FH-11: a reply tapped in a confirm card's blocks (below its receipt) is
// sent by the card, with the tap's index in the ask. It used to drop
// whether the reply went, so BlockList kept it "sent" after a failure. The
// card hands the result back now. BlockList stands in for itself, to catch
// what the card gives it.

const lists = vi.hoisted(() => [] as BlockListProps[])
const sent = vi.hoisted(() => [] as { text: string; tap?: Tap }[])
vi.mock('../blocks/BlockList', () => ({
  BlockList: (p: BlockListProps) => {
    lists.push(p)
    return null
  },
}))
vi.mock('../../state/actions', () => ({
  reply: async (text: string, tap?: Tap) => {
    sent.push({ text, tap })
    return false
  },
}))

const idle: Reply = { busy: false, sent: false, which: '', error: '', send: async () => true, retry() {} }

describe('ConfirmCard', () => {
  it('hands back whether a reply tapped below the receipt went', async () => {
    const ask = askFromEntry({
      id: 7,
      kind: 'ask',
      text: 'Place this order?',
      createdAt: 0,
      updatedAt: 0,
      data: { confirm: true, options: ['Place order', 'Cancel'], blocks: [receipt, { type: 'cards', kind: 'item', items: [{ title: 'Pickup instead', action: { do: 'reply', label: 'Pick up', text: 'Pick it up instead' } }] }] },
    })
    renderToStaticMarkup(<ConfirmCard ask={ask} reply={idle} answer={() => {}} />)
    const onReply = lists[lists.length - 1].onReply
    expect(onReply).toBeDefined()
    const went = onReply!('Pick it up instead', { entryId: 7, block: 0, item: 0 })
    expect(await went).toBe(false)
    // The tap names the block's place in the ask, after the receipt.
    expect(sent).toEqual([{ text: 'Pick it up instead', tap: { entryId: 7, block: 1, item: 0 } }])
  })
})
