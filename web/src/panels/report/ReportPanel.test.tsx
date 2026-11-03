import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import type { Block } from '../../api/blocks'
import type { Report } from '../../api/types'
import type { BlockListProps } from '../../features/blocks/BlockList'
import { ReportView } from './ReportPanel'

// The blocks stand in for themselves here: what matters is the reply the
// report hands them, which a tap on a reply card calls.
const lists = vi.hoisted(() => [] as BlockListProps[])
vi.mock('../../features/blocks/BlockList', () => ({
  BlockList: (p: BlockListProps) => {
    lists.push(p)
    return null
  },
}))

const cards: Block = { type: 'cards', kind: 'stay', items: [{ title: 'Casa Alfama', action: { do: 'reply', label: 'Book', text: 'Book Casa Alfama' } }] }
const report: Report = { id: 'r1', title: 'Lisbon hotels', format: 'markdown', content: 'Three quiet ones.', data: { blocks: [cards] }, createdAt: 1, updatedAt: 1 }

describe('ReportView', () => {
  it('sends a reply card in the report as a message from the report', () => {
    const sent: string[] = []
    lists.length = 0
    renderToStaticMarkup(<ReportView report={report} onClose={() => {}} onSend={(t) => void sent.push(t)} />)
    expect(lists).toHaveLength(1)
    expect(lists[0].placement).toBe('report')
    lists[0].onReply?.('Book Casa Alfama', { entryId: 0, block: 0, item: 0 })
    expect(sent).toEqual(['Book Casa Alfama'])
  })
})
