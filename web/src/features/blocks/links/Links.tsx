import type { BlockOf } from '../../../api/blocks'
import { api } from '../../../api/client'
import { hostOf } from '../../../lib/url'
import { Favicon } from '../../../ui/Favicon'
import { Download, External, File, Image as ImageGlyph } from '../../../ui/icons'
import { safeURL } from '../parts/Action'
import { Head } from '../parts/Head'
import './links.css'

// Links are pages worth opening: the site's icon, the title, the site and
// Alexa's note, one row each.
export function Links({ block: b }: { block: BlockOf<'links'> }) {
  return (
    <>
      <Head block={b} />
      <ul className="b-surface b-rows" role="list">
        {b.items.map((it, i) => {
          const href = safeURL(it.url)
          if (!href) return null
          const host = hostOf(href)
          return (
            <li key={i}>
              <a className="b-row" href={href} target="_blank" rel="noopener noreferrer">
                <Favicon host={host} size={20} />
                <span className="b-row-t">
                  <b>{it.title}</b>
                  <span>
                    <span className="b-host">{host}</span>
                    {it.note && <span className="b-dot">{it.note}</span>}
                  </span>
                </span>
                <External size={16} />
              </a>
            </li>
          )
        })}
      </ul>
    </>
  )
}

const pictures = /\.(png|jpe?g|gif|webp|avif|heic|svg)$/i

// Files are files in the workspace Alexa made or fetched: a tile with the
// type, the name and the folder, and a download.
export function Files({ block: b }: { block: BlockOf<'files'> }) {
  return (
    <>
      <Head block={b} />
      <ul className="b-surface b-rows" role="list">
        {b.items.map((path, i) => {
          const name = path.split('/').pop() || path
          const dir = path.slice(0, path.length - name.length).replace(/\/$/, '')
          const ext = /\.([a-z0-9]{1,5})$/i.exec(name)?.[1]
          return (
            <li key={i}>
              <a className="b-row" href={api.fileURL(path)} download={name} aria-label={`Download ${name}`}>
                <span className="b-ftile">{pictures.test(name) ? <ImageGlyph size={18} /> : <File size={18} />}</span>
                <span className="b-row-t">
                  <b>{name}</b>
                  {(ext || dir) && (
                    <span>
                      {ext && <span>{ext.toUpperCase()}</span>}
                      {dir && <span className="b-dot">{dir}</span>}
                    </span>
                  )}
                </span>
                <Download size={16} />
              </a>
            </li>
          )
        })}
      </ul>
    </>
  )
}
