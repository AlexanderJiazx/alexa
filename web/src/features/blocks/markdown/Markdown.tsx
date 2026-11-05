import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { BlockOf } from '../../../api/blocks'
import { MdImage } from '../parts/MdImage'
import './markdown.css'

const plugins = [remarkGfm]
const components = {
  a: ({ href, children }: { href?: string; children?: React.ReactNode }) => (
    <a href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  ),
  img: MdImage,
}

// MarkdownBlock is prose between a report's blocks, set like the rest of
// the report (.prose). Raw HTML in it isn't rendered, and images load only
// from the server or the workspace (MdImage).
export function MarkdownBlock({ block: b }: { block: BlockOf<'markdown'> }) {
  return (
    <div className="prose b-markdown">
      <Markdown remarkPlugins={plugins} components={components}>
        {b.text}
      </Markdown>
    </div>
  )
}
