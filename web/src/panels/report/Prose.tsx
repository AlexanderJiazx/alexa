import { memo, type ComponentProps } from 'react'
import Markdown, { type Components, type ExtraProps } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { MdImage } from '../../features/blocks/parts/MdImage'
import { cx } from '../../ui/cx'
import { Favicon, hostName } from '../../ui/Favicon'
import rehypeReport from './rehypeReport'

const remark = [remarkGfm]
const rehype = [rehypeReport]

type A = ComponentProps<'a'> & ExtraProps

// LinkPill is a link that would only show its own address: the site's icon
// and name, with the path after it, quieter.
export function LinkPill({ href }: { href: string }) {
  let host = hostName(href)
  let path = ''
  try {
    const u = new URL(href)
    host = u.hostname.replace(/^www\./, '')
    path = decodeURI(u.pathname + u.search).replace(/\/+$/, '')
  } catch {
    /* not a URL: the host is the text */
  }
  return (
    <a className="pill-link" href={href} target="_blank" rel="noopener noreferrer" title={href}>
      <Favicon host={host} size={14} />
      <span className="pl-host">{host}</span>
      {path && <span className="pl-path">{path}</span>}
    </a>
  )
}

function Anchor({ node: _, href = '', className, children, ...rest }: A) {
  if (className === 'pill' && href) return <LinkPill href={href} />
  // Footnotes and other links within the report stay in place; the rest
  // open in a new tab.
  const away = !href.startsWith('#')
  return (
    <a {...rest} className={className} href={href} {...(away ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>
      {children}
    </a>
  )
}

// The report has one h1, its title; a "#" heading in the content is a part
// heading, a step above the sections.
function PartHeading({ node: _, className, ...rest }: ComponentProps<'h1'> & ExtraProps) {
  return <h2 {...rest} className={cx('part', className)} />
}

// Images load only from the server's proxy or the workspace (MdImage).
const components: Components = { a: Anchor, h1: PartHeading, img: MdImage }

// Prose is a stretch of a report's markdown, set for reading. lede sets it
// as the report's opening paragraph.
export const Prose = memo(function Prose({ text, lede }: { text: string; lede?: boolean }) {
  return (
    <div className={cx('prose', lede && 'lede')}>
      <Markdown remarkPlugins={remark} rehypePlugins={rehype} components={components}>
        {text}
      </Markdown>
    </div>
  )
})
