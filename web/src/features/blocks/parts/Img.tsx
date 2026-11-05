import { useState, type CSSProperties, type ReactNode } from 'react'
import { api } from '../../../api/client'
import { cx } from '../../../ui/cx'
import { Image as ImageGlyph } from '../../../ui/icons'

type Props = {
  src: string
  alt: string
  // ratio is the box's width / height, so nothing shifts as it loads.
  ratio: number
  // contain fits the whole picture on the tile (products); cover fills.
  fit?: 'cover' | 'contain'
  // fallback shows if the image can't load: a monogram, say.
  fallback?: ReactNode
  className?: string
}

// Img is a block image: remote pictures come through the server's proxy
// (/api/img/{token}) and workspace files from the workspace, in a fixed
// aspect box with a skeleton while loading and a quiet tile if it fails.
export function Img({ src, alt, ratio, fit = 'cover', fallback, className }: Props) {
  const [state, setState] = useState<'loading' | 'ok' | 'gone'>('loading')
  return (
    <span className={cx('b-img', fit, state, className)} style={{ '--ratio': ratio } as CSSProperties}>
      {state === 'gone' ? (
        <span className="b-img-none">{fallback ?? <ImageGlyph size={20} />}</span>
      ) : (
        <img
          ref={(el) => {
            // A cached picture can finish before React hears its load.
            if (el?.complete && el.naturalWidth && state === 'loading') setState('ok')
          }}
          src={api.imgURL(src)}
          alt={alt}
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          draggable={false}
          onLoad={() => setState('ok')}
          onError={() => setState('gone')}
        />
      )}
    </span>
  )
}

// Monogram is the stand-in for a place or stay with no picture: its first
// letter on a tile.
export function Monogram({ name }: { name: string }) {
  const ch = name.match(/[\p{L}\p{N}]/u)?.[0]?.toUpperCase() ?? '·'
  return (
    <span className="b-mono" aria-hidden="true">
      {ch}
    </span>
  )
}
