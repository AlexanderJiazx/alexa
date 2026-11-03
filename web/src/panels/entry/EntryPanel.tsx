import { BlockList } from '../../features/blocks/BlockList'
import { headTitle } from '../../features/blocks/summary'
import { ago } from '../../lib/format'
import { closePanel } from '../../state/actions'
import { restedAt, split } from '../../state/exchange'
import { useEntry } from '../../state/selectors'
import { useApp } from '../../state/store'
import { IconButton } from '../../ui/IconButton'
import { Close, Expand } from '../../ui/icons'
import type { PanelProps } from '../registry'
import './entry.css'

// EntryPanel is the Expand target: one entry's blocks (a comparison, a
// table, an itinerary) given the panel's room (Surface {kind:"entry", id},
// WP-04). What Alexa said sits above them as the lede. Replies still work
// while the entry is part of the current topic.
export function EntryPanel({ id }: PanelProps) {
  const n = Number(id)
  const entry = useEntry(n)
  const live = useApp((s) => split(s.data.feed, restedAt()).topic.some((e) => e.id === n))
  const blocks = entry?.data?.blocks
  const title = blocks?.map(headTitle).find(Boolean) || 'Details'
  // When the first block's title heads the panel, it isn't said twice.
  const named = !!blocks?.length && headTitle(blocks[0]) === title
  return (
    <section className="panel" aria-label={title}>
      <div className="panel-head">
        <span className="panel-ic">
          <Expand size={16} />
        </span>
        <span className="title">
          {title}
          {entry && <span className="sub">{ago(entry.updatedAt || entry.createdAt)}</span>}
        </span>
        <IconButton label="Close" onClick={closePanel}>
          <Close size={18} />
        </IconButton>
      </div>
      <div className="panel-body doc">
        {entry ? (
          <div className={named ? 'entry-doc named' : 'entry-doc'}>
            {entry.text && <p className="entry-lede">{entry.text}</p>}
            <BlockList blocks={blocks} placement="panel" frozen={!live} entryId={entry.id} echo />
          </div>
        ) : (
          <p className="entry-gone">This isn’t loaded any more. It’s still in Reflect.</p>
        )}
      </div>
    </section>
  )
}
