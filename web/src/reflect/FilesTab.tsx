import { useRef, useState, type DragEvent } from 'react'
import { api } from '../api/client'
import type { FileInfo } from '../api/types'
import { ago, fmtSize } from '../lib/format'
import { failed, LoadError, RowSkeletons, useList } from '../sheets/parts'
import { notify } from '../sheets/toasts'
import { removeLater, usePending } from '../sheets/undo'
import { useChanged } from '../state/selectors'
import { Button } from '../ui/Button'
import { cx } from '../ui/cx'
import { IconButton } from '../ui/IconButton'
import { Download, File as FileIcon, Image, Report, Trash } from '../ui/icons'

const images = /\.(png|jpe?g|gif|webp|avif|svg|heic)$/i
const docs = /\.(md|txt|pdf|docx?|html?|csv|json)$/i

// split shows a path as its name, with the folder it's in beside it.
export function split(path: string) {
  const i = path.lastIndexOf('/')
  return i < 0 ? { name: path, folder: '' } : { name: path.slice(i + 1), folder: path.slice(0, i) }
}

const loadFiles = () => api.files().then((f) => f.filter((x) => !x.isDir).sort((a, b) => b.modTime - a.modTime))

// Files is the user's private workspace: uploads, downloads and what Alexa
// makes. Drop files anywhere on it to upload them.
export function Files({ query }: { query: string }) {
  const changed = useChanged('files')
  const { list, error, reload } = useList(loadFiles, [changed])
  const pending = usePending()
  const [uploading, setUploading] = useState(0)
  const [over, setOver] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const q = query.trim().toLowerCase()
  // What's waiting to be deleted is gone as far as the list is concerned.
  const all = (list ?? []).filter((f) => !pending.has(`file:${f.path}`))
  const kept = all.length
  const shown = all.filter((f) => !q || f.path.toLowerCase().includes(q))

  const upload = async (files: FileList | File[] | null) => {
    const all = Array.from(files ?? [])
    if (!all.length) return
    setUploading((n) => n + all.length)
    let ok = 0
    for (const f of all) {
      try {
        await api.upload(f)
        ok++
      } catch (e) {
        failed(`Couldn’t upload ${f.name}`)(e as Error)
      } finally {
        setUploading((n) => n - 1)
      }
    }
    if (ok) notify({ title: ok === 1 ? 'Uploaded' : `Uploaded ${ok} files`, body: ok === 1 ? all[0].name : undefined, tone: 'ok' })
    reload()
  }

  const remove = (f: FileInfo) =>
    removeLater({
      key: `file:${f.path}`,
      title: 'File deleted',
      body: split(f.path).name,
      commit: () => api.deleteFile(f.path),
      done: reload,
      failed: failed('Couldn’t delete that file'),
    })

  const drop = {
    onDragOver: (e: DragEvent) => {
      if (!e.dataTransfer.types.includes('Files')) return
      e.preventDefault()
      setOver(true)
    },
    onDragLeave: (e: DragEvent) => {
      if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false)
    },
    onDrop: (e: DragEvent) => {
      e.preventDefault()
      setOver(false)
      upload(e.dataTransfer.files)
    },
  }

  const picker = (
    <input
      ref={input}
      type="file"
      multiple
      className="sr"
      tabIndex={-1}
      onChange={(e) => {
        upload(e.target.files)
        e.target.value = ''
      }}
    />
  )

  return (
    <div className={cx('reflect-scroll', over && 'drop-over')} {...drop}>
      <div className="reflect-pane">
        {picker}
        {error && !list ? (
          <LoadError message={error} onRetry={reload} />
        ) : list === null ? (
          <RowSkeletons count={4} />
        ) : kept === 0 && !q ? (
          <div className="drop-zone">
            <FileIcon size={28} />
            <p className="drop-title">Drop files here</p>
            <p className="drop-body">Alexa can read what you upload, and what it downloads or makes for you is kept here too.</p>
            <Button variant="primary" loading={uploading > 0} onClick={() => input.current?.click()}>
              Upload
            </Button>
          </div>
        ) : (
          <>
            <div className="files-head">
              <span className="files-count num">{q ? `${shown.length} of ${kept}` : `${kept} ${kept === 1 ? 'file' : 'files'}`}</span>
              <Button size="sm" loading={uploading > 0} onClick={() => input.current?.click()}>
                Upload
              </Button>
            </div>
            {shown.length === 0 ? (
              <p className="empty-note">No file is called anything like “{query.trim()}”.</p>
            ) : (
              <ul className="group row-list files">
                {shown.map((f) => {
                  const { name, folder } = split(f.path)
                  return (
                    <li key={f.path} className="row file">
                      <span className="file-ic" aria-hidden="true">
                        {images.test(name) ? <Image size={18} /> : docs.test(name) ? <Report size={18} /> : <FileIcon size={18} />}
                      </span>
                      <a className="row-text file-main" href={api.fileURL(f.path, true)} target="_blank" rel="noopener noreferrer">
                        <span className="row-label">{name}</span>
                        <span className="row-sub">
                          {folder}
                          {/* On a phone the size moves under the name, so the name has the room. */}
                          <span className="file-size-sub num">
                            {folder && ' · '}
                            {fmtSize(f.size)}
                          </span>
                        </span>
                      </a>
                      <span className="file-size num">{fmtSize(f.size)}</span>
                      <span className="file-when num">{ago(f.modTime)}</span>
                      <span className="row-end file-actions">
                        <a className="icon-btn sm muted" href={api.fileURL(f.path)} download aria-label={`Download ${name}`} title="Download">
                          <Download size={16} />
                        </a>
                        <IconButton size="sm" tone="muted" label={`Delete ${name}`} title="Delete" onClick={() => remove(f)}>
                          <Trash size={16} />
                        </IconButton>
                      </span>
                    </li>
                  )
                })}
              </ul>
            )}
          </>
        )}
      </div>
    </div>
  )
}
