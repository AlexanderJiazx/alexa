import { api } from '../../../api/client'
import { cx } from '../../../ui/cx'
import { File, Report } from '../../../ui/icons'
import type { EntryProps } from '../registry'
import { Speaker } from './Speaker'

// UserEntry is what the user said, with the files they attached above it:
// pictures as thumbnails, other files by name. A submitted form or a
// multi-select reads as the quiet summary it is; a message a report page
// sent says it came from there, so it's visibly the user's choice.
export function UserEntry({ entry: e }: EntryProps) {
  const atts = e.data?.attachments ?? []
  const pics = atts.filter((a) => a.mime?.startsWith('image/'))
  const files = atts.filter((a) => !a.mime?.startsWith('image/'))
  const via = e.data?.via
  return (
    <div className={cx('msg you', (via === 'form' || via === 'multi') && 'summary')}>
      <Speaker who="you" />
      {via === 'report' && (
        <span className="msg-via">
          <Report size={12} />
          From the report
        </span>
      )}
      {pics.length > 0 && (
        <div className="att-pics">
          {pics.map((a) => (
            <a key={a.path} className="att-pic" href={api.fileURL(a.path, true)} target="_blank" rel="noopener noreferrer" title={a.name}>
              <img src={api.fileURL(a.path, true)} alt={a.name} loading="lazy" />
            </a>
          ))}
        </div>
      )}
      {files.map((a) => (
        <a key={a.path} className="att" href={api.fileURL(a.path, true)} target="_blank" rel="noopener noreferrer">
          <File size={14} />
          <span>{a.name}</span>
        </a>
      ))}
      {e.text && <p>{e.text}</p>}
    </div>
  )
}
