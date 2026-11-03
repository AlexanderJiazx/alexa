import { useEffect, useState, type KeyboardEvent, type MouseEvent } from 'react'
import type { BlockOf } from '../../../api/blocks'
import { api } from '../../../api/client'
import { IconButton } from '../../../ui/IconButton'
import { Back, Close, External, Fwd } from '../../../ui/icons'
import { useModal } from '../../../ui/useModal'
import { safeURL } from '../parts/Action'

type Item = BlockOf<'images'>['items'][number]

// Lightbox shows one picture large on a dark scrim, as a native modal
// dialog: focus stays inside, Escape or a click beside the picture closes
// it, and focus goes back to the picture that opened it. The arrow keys
// move through the gallery.
export function Lightbox({ items, at, onClose }: { items: Item[]; at: number; onClose: () => void }) {
  const [open, setOpen] = useState(true)
  const [i, setI] = useState(at)
  const close = () => setOpen(false)
  const m = useModal(open, close, 120)
  // The dialog plays its exit, then the owner forgets it.
  useEffect(() => {
    if (!open && !m.shown) onClose()
  }, [open, m.shown, onClose])
  const it = items[i]
  const many = items.length > 1
  const go = (d: number) => setI((x) => (x + d + items.length) % items.length)
  const onKeyDown = (e: KeyboardEvent<HTMLDialogElement>) => {
    if (many && e.key === 'ArrowRight') go(1)
    else if (many && e.key === 'ArrowLeft') go(-1)
    else m.onKeyDown(e)
  }
  // The picture fills the screen, so "outside" is any bare backdrop.
  const onBare = (e: MouseEvent) => {
    if (e.target === e.currentTarget) close()
  }
  if (!m.shown || !it) return null
  const link = safeURL(it.url)
  return (
    <dialog ref={m.ref} className={`b-lightbox${m.leaving ? ' leaving' : ''}`} aria-label={it.caption} onKeyDown={onKeyDown} onClick={onBare}>
      <div className="b-lb-bar">
        {many && (
          <span className="b-lb-count num">
            {i + 1} of {items.length}
          </span>
        )}
        {link && (
          <a className="b-lb-link" href={link} target="_blank" rel="noopener noreferrer">
            Open page
            <External size={14} />
          </a>
        )}
        <IconButton label="Close" className="b-lb-close" onClick={close}>
          <Close size={20} />
        </IconButton>
      </div>
      <figure className="b-lb-fig" onClick={onBare}>
        <img key={i} src={api.imgURL(it.src)} alt={it.caption} referrerPolicy="no-referrer" decoding="async" />
        <figcaption>{it.caption}</figcaption>
      </figure>
      {many && (
        <>
          <IconButton label="Previous" className="b-lb-nav prev" onClick={() => go(-1)}>
            <Back size={22} />
          </IconButton>
          <IconButton label="Next" className="b-lb-nav next" onClick={() => go(1)}>
            <Fwd size={22} />
          </IconButton>
        </>
      )}
    </dialog>
  )
}
