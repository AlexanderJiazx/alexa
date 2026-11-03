import { useState } from 'react'
import type { BlockOf } from '../../../api/blocks'
import { Carousel } from '../parts/Carousel'
import { Head } from '../parts/Head'
import { Img } from '../parts/Img'
import { summarize } from '../summary'
import { Lightbox } from './Lightbox'
import './images.css'

type B = BlockOf<'images'>

// Images are a gallery: one picture full width, two to four in a grid,
// more in a carousel. Each opens the lightbox; the caption is its alt text.
export function Images({ block: b }: { block: B }) {
  const [open, setOpen] = useState<number | null>(null)
  const n = b.items.length
  const tiles = b.items.map((it, i) => (
    <li key={i} className="b-shot">
      <button type="button" className="b-shot-btn" onClick={() => setOpen(i)} aria-label={`${it.caption}, open larger`} aria-haspopup="dialog">
        <Img src={it.src} alt={it.caption} ratio={n === 1 ? 16 / 9 : 3 / 2} />
      </button>
      <span className="b-shot-cap">{it.caption}</span>
    </li>
  ))
  return (
    <>
      <Head block={b} />
      {n > 4 ? (
        <Carousel label={b.title || summarize(b)} className="b-shots-row">
          {tiles}
        </Carousel>
      ) : (
        <ul className="b-shots" role="list" data-n={n}>
          {tiles}
        </ul>
      )}
      {open !== null && <Lightbox items={b.items} at={open} onClose={() => setOpen(null)} />}
    </>
  )
}
