import { useRef, useState } from 'react'
import type { Source } from '../../api/blocks'
import { hostOf } from '../../lib/url'
import { cx } from '../../ui/cx'
import { Favicon } from '../../ui/Favicon'
import { Menu } from '../../ui/Menu'
import { External } from '../../ui/icons'
import { safeURL } from './parts/Action'
import './sources.css'

// Sources is the row under a say that says where Alexa looked: up to four
// site icons overlapping and "4 sources". It opens a short list of the
// pages, each a link: above the row, or below it when the row sits too
// near the top of the screen for the list to fit.
export function Sources({ sources }: { sources?: Source[] }) {
  const [open, setOpen] = useState(false)
  const [below, setBelow] = useState(false)
  const btn = useRef<HTMLButtonElement>(null)
  const list = (Array.isArray(sources) ? sources : []).map((s) => ({ ...s, url: safeURL(s?.url) })).filter((s): s is Source => !!s.url)
  if (!list.length) return null
  const hosts = [...new Set(list.map((s) => hostOf(s.url)))]
  const label = list.length === 1 ? hosts[0] : `${list.length} sources`
  const toggle = () => {
    const r = btn.current?.getBoundingClientRect()
    // Each row is 52px; the header covers the top 52px of the screen.
    if (r) setBelow(r.top - 52 < Math.min(400, list.length * 52 + 16) && innerHeight - r.bottom > r.top)
    setOpen(!open)
  }
  return (
    <div className="b-sources">
      <button ref={btn} type="button" className="b-sources-btn" aria-haspopup="menu" aria-expanded={open} onClick={toggle}>
        <span className="favicons">
          {hosts.slice(0, 4).map((h) => (
            <Favicon key={h} host={h} size={16} round />
          ))}
        </span>
        <span>{label}</span>
      </button>
      {open && (
        <Menu label="Sources" onClose={() => setOpen(false)} className={cx('b-sources-menu', below && 'below')}>
          {list.map((s, i) => {
            const host = hostOf(s.url)
            return (
              <a key={i} role="menuitem" tabIndex={-1} className="b-source" href={s.url} target="_blank" rel="noopener noreferrer" onClick={() => setOpen(false)}>
                <Favicon host={host} size={20} />
                <span className="b-source-t">
                  <b>{s.title || host}</b>
                  <span>{host}</span>
                </span>
                <External size={14} />
              </a>
            )
          })}
        </Menu>
      )}
    </div>
  )
}
