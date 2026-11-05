import type { ComponentType } from 'react'
import type { Block, BlockType } from '../../api/blocks'
import { Callout } from './callout/Callout'
import { Cards } from './cards/Cards'
import { Chart } from './chart/Chart'
import { Checklist } from './checklist/Checklist'
import { Compare } from './compare/Compare'
import { Images } from './images/Images'
import { Files, Links } from './links/Links'
import { MarkdownBlock } from './markdown/Markdown'
import { Receipt } from './receipt/Receipt'
import { Stats } from './stats/Stats'
import { Table } from './table/Table'
import { Timeline } from './timeline/Timeline'

// The renderer for each block type (Appendix A.2). Adding a type is a
// folder with its renderer and stylesheet, a line here, and its check in
// valid.ts.
export const renderers: Record<BlockType, ComponentType<{ block: Block }>> = {
  cards: Cards,
  compare: Compare,
  table: Table,
  stats: Stats,
  chart: Chart,
  timeline: Timeline,
  checklist: Checklist,
  receipt: Receipt,
  links: Links,
  images: Images,
  files: Files,
  callout: Callout,
  markdown: MarkdownBlock,
} as Record<BlockType, ComponentType<{ block: Block }>>
