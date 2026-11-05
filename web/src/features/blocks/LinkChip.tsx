import { hostOf } from '../../lib/url'
import { Favicon } from '../../ui/Favicon'
import './sources.css'

// LinkChip is a bare URL in what Alexa said, shown as the site's icon, its
// host and a short path instead of raw text (research-genui 5.5). The
// conversation's linkify can render it in place of a plain link.
export function LinkChip({ url }: { url: string }) {
  let path = ''
  try {
    const u = new URL(url)
    path = (u.pathname + u.search).replace(/\/$/, '')
  } catch {
    return <>{url}</>
  }
  const host = hostOf(url)
  const short = path.length > 24 ? `${path.slice(0, 23)}…` : path
  return (
    <a className="b-linkchip" href={url} target="_blank" rel="noopener noreferrer" title={url}>
      <Favicon host={host} size={14} />
      <span className="h">{host}</span>
      {short && <span className="p">{short}</span>}
    </a>
  )
}
