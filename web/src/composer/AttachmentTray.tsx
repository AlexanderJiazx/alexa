import { api } from '../api/client'
import type { Attachment } from '../api/types'
import { fmtSize } from '../lib/format'
import { cx } from '../ui/cx'
import { Alert, Close, File } from '../ui/icons'

// Upload is a file on its way up, or one that couldn't go.
export type Upload = { key: string; name: string; preview?: string; error?: string }

// An attached file, with the server's note when Alexa can't look at it as a
// picture ("Alexa can’t look at SVG drawings, only read their code.").
export type Attached = Attachment & { note?: string }

type Props = {
  files: Attached[]
  uploads: Upload[]
  onRemove: (f: Attachment) => void
  onDismiss: (u: Upload) => void
}

const isImage = (mime?: string) => !!mime && mime.startsWith('image/')

// AttachmentTray sits above the text, inside the composer: a thumbnail for
// each image, a tile with the name for other files, a spinner while one
// uploads, and a × to take one off (FE-15).
export function AttachmentTray({ files, uploads, onRemove, onDismiss }: Props) {
  if (!files.length && !uploads.length) return null
  const notes = files.filter((f) => f.note)
  return (
    <>
      <ul className="tray" aria-label="Attached files">
        {files.map((f) => (
          <li key={f.path} className={cx('tray-item', isImage(f.mime) && 'img')}>
            {isImage(f.mime) ? <img src={api.fileURL(f.path, true)} alt="" /> : <TileText name={f.name} meta={fmtSize(f.size)} />}
            <button type="button" className="tray-x" aria-label={`Remove ${f.name}`} title={`Remove ${f.name}`} onClick={() => onRemove(f)}>
              <Close size={12} />
            </button>
          </li>
        ))}
        {uploads.map((u) => (
          <li key={u.key} className={cx('tray-item', u.preview && !u.error && 'img', u.error ? 'failed' : 'busy')} aria-busy={!u.error || undefined}>
            {u.preview && !u.error ? <img src={u.preview} alt="" /> : <TileText name={u.name} meta={u.error ? 'Couldn’t upload' : 'Uploading…'} bad={!!u.error} />}
            {!u.error && <span className="spinner" aria-label={`Uploading ${u.name}`} />}
            {u.error && (
              <button type="button" className="tray-x" aria-label={`Dismiss ${u.name}`} title={u.error} onClick={() => onDismiss(u)}>
                <Close size={12} />
              </button>
            )}
          </li>
        ))}
      </ul>
      {notes.map((f) => (
        <p key={f.path} className="tray-note">
          <Alert size={14} />
          <span>
            {f.name}: {f.note}
          </span>
        </p>
      ))}
    </>
  )
}

function TileText({ name, meta, bad }: { name: string; meta: string; bad?: boolean }) {
  return (
    <span className="tray-file">
      <span className="tray-ic">{bad ? <Alert size={16} /> : <File size={16} />}</span>
      <span className="tray-name">{name}</span>
      <span className="tray-meta num">{meta}</span>
    </span>
  )
}
