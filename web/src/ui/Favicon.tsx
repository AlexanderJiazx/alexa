import { useState, type CSSProperties } from 'react'
import { api } from '../api/client'
import { cx } from './cx'

// hostName is the bare host of a URL or host ("https://www.bbc.co.uk/x" →
// "bbc.co.uk").
export function hostName(s: string) {
  const t = s.trim()
  try {
    return new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(t) ? t : `https://${t}`).hostname.replace(/^www\./, '')
  } catch {
    return t.replace(/^www\./, '')
  }
}

// letterFor is the fallback tile's letter: the first letter of the site's
// name, skipping subdomains like m. and en.
export function letterFor(host: string) {
  const parts = hostName(host).split('.').filter(Boolean)
  const name = parts.length > 2 && parts[0].length <= 3 ? parts[1] : parts[0]
  const ch = (name || '?').match(/[\p{L}\p{N}]/u)?.[0] ?? '?'
  return ch.toUpperCase()
}

// Favicon is a site's icon, fetched through the server (which only goes to
// public addresses), with a letter tile while it loads or if it has none.
export function Favicon({ host, size = 16, round, className }: { host: string; size?: number; round?: boolean; className?: string }) {
  const h = hostName(host)
  const [failed, setFailed] = useState(false)
  const [loaded, setLoaded] = useState(false)
  return (
    <span className={cx('favicon', round && 'round', loaded && !failed && 'loaded', className)} style={{ '--size': `${size}px` } as CSSProperties} title={h} aria-hidden="true">
      <span className="favicon-letter">{letterFor(h)}</span>
      {h && !failed && <img src={api.faviconURL(h)} alt="" width={size} height={size} loading="lazy" decoding="async" onLoad={() => setLoaded(true)} onError={() => setFailed(true)} />}
    </span>
  )
}
