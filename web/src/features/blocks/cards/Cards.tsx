import type { BlockOf } from '../../../api/blocks'
import { useBlocks } from '../context'
import { Carousel } from '../parts/Carousel'
import { Head } from '../parts/Head'
import { summarize } from '../summary'
import { CardView, type Kind } from './Card'
import { FlightCard } from './Flight'
import './cards.css'

const nouns: Record<Kind, string> = { product: 'products', place: 'places', stay: 'stays', flight: 'flights', event: 'events', item: 'options' }

// Cards lays out 1 to 8 things of one kind: one is a wide row; two or
// three sit side by side when there's room and stack when there isn't;
// four or more scroll sideways in the exchange and form a grid in the
// panel and the report. Flights always stack, like a departures board.
export function Cards({ block: b }: { block: BlockOf<'cards'> }) {
  const ctx = useBlocks()
  const kind: Kind = b.kind && b.kind in nouns ? b.kind : 'item'
  const n = b.items.length
  const layout = kind === 'flight' ? 'stack' : n === 1 ? 'one' : n <= 3 || ctx.placement !== 'inline' ? 'grid' : 'carousel'
  const items = b.items.map((c, i) => (
    <li key={i} className="b-cell">
      {kind === 'flight' ? <FlightCard c={c} i={i} /> : <CardView c={c} kind={kind} i={i} />}
    </li>
  ))
  return (
    <>
      <Head block={b} meta={b.title && n > 1 ? `${n} ${nouns[kind]}` : undefined} />
      {layout === 'carousel' ? (
        <Carousel label={b.title || summarize(b)}>{items}</Carousel>
      ) : (
        <ul className={`b-cards ${layout}`} role="list" data-n={Math.min(n, 4)}>
          {items}
        </ul>
      )}
    </>
  )
}
