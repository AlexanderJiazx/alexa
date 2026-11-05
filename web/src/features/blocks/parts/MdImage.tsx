import type { ComponentProps } from 'react'
import type { ExtraProps } from 'react-markdown'
import { api } from '../../../api/client'
import { hostName } from '../../../ui/Favicon'
import { Image as ImageGlyph } from '../../../ui/icons'
import './md-image.css'

// What a markdown image may load: a picture the server has already proxied
// (/api/img/{token}), or an image file in the workspace. Anything else would
// have the user's browser fetch it from its own host, telling that host who
// is reading and when (a page Alexa read can plant one), so it never loads.
const proxied = /^\/api\/img\/[0-9a-f]{32}$/
const imageFile = /\.(png|jpe?g|gif|webp|avif)$/i

// mdImageSrc is what an <img> may load for a markdown image's src, or ''
// when it mustn't load at all.
export function mdImageSrc(src: string | undefined): string {
  const s = (src ?? '').trim()
  if (!s) return ''
  if (proxied.test(s)) return s
  // A workspace path: relative, no scheme, not climbing out.
  if (/^[a-z][a-z0-9+.-]*:/i.test(s) || s.startsWith('/') || s.startsWith('\\') || s.split(/[\\/]/).includes('..')) return ''
  return imageFile.test(s.split(/[?#]/)[0]) ? api.imgURL(s) : ''
}

type Props = ComponentProps<'img'> & ExtraProps

// MdImage is the img of a report's markdown: loaded only from the server or
// the workspace; a remote picture shows as a quiet note of what and where
// it was instead.
export function MdImage({ src, alt }: Props) {
  const ok = mdImageSrc(typeof src === 'string' ? src : undefined)
  if (ok) return <img src={ok} alt={alt ?? ''} loading="lazy" decoding="async" referrerPolicy="no-referrer" />
  const host = typeof src === 'string' && /^https?:\/\//i.test(src) ? hostName(src) : ''
  return (
    <span className="md-img-off" title={host ? `An image from ${host}, not loaded` : 'An image, not loaded'}>
      <ImageGlyph size={14} />
      <span>{alt?.trim() || 'Image'}</span>
      {host && <span className="md-img-host">{host}</span>}
    </span>
  )
}
