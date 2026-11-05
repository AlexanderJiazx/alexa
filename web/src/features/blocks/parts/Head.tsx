import type { ReactNode } from 'react'
import type { Block } from '../../../api/blocks'
import { IconButton } from '../../../ui/IconButton'
import { Expand } from '../../../ui/icons'
import { useBlocks } from '../context'
import { expandable, headTitle } from '../summary'

// Head is a block's heading: its title, a quiet count or range, and for a
// comparison, a table or an itinerary in the exchange, Expand, which opens
// the block in the panel with room to breathe.
export function Head({ block, meta, children }: { block: Block; meta?: ReactNode; children?: ReactNode }) {
  const ctx = useBlocks()
  const title = headTitle(block)
  const canExpand = ctx.placement === 'inline' && ctx.entryId !== undefined && expandable(block)
  if (!title && !meta && !canExpand && !children) return null
  return (
    <div className="b-head">
      {title && <h3 className="b-title">{title}</h3>}
      {meta && <span className="b-meta">{meta}</span>}
      {children}
      {canExpand && (
        <IconButton size="sm" tone="muted" className="b-expand" label="Expand" onClick={ctx.expand}>
          <Expand size={16} />
        </IconButton>
      )}
    </div>
  )
}
