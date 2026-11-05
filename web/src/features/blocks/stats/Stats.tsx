import type { BlockOf } from '../../../api/blocks'
import { minus } from '../../../lib/format'
import { Pill, type Tone } from '../../../ui/Pill'
import { TrendDown, TrendFlat, TrendUp } from '../icons'
import { Head } from '../parts/Head'
import './stats.css'

type Item = BlockOf<'stats'>['items'][number]

// tone colours a change by whether it's good: a fare going down is good,
// spending going up isn't. Without a good direction it stays neutral.
export function tone(s: Pick<Item, 'trend' | 'good'>): Tone {
  if (!s.trend || s.trend === 'flat' || !s.good) return 'neutral'
  return s.trend === s.good ? 'ok' : 'bad'
}

const arrows = { up: TrendUp, down: TrendDown, flat: TrendFlat }

// Stats are 1 to 6 headline numbers on one surface, divided by hairlines:
// the label, the figure large in tabular type, then how it moved (an
// arrow, the change and a word for screen readers) and a quiet note.
export function Stats({ block: b }: { block: BlockOf<'stats'> }) {
  const n = b.items.length
  return (
    <>
      <Head block={b} />
      <div className="b-surface b-stats" data-n={n}>
        {b.items.map((s, i) => {
          const t = tone(s)
          const A = s.trend ? arrows[s.trend] : null
          return (
            <div key={i} className="b-stat">
              <span className="b-stat-label">{s.label}</span>
              <span className="b-stat-value">{s.value}</span>
              {(s.delta || s.note) && (
                <span className="b-stat-foot">
                  {s.delta && (
                    <Pill tone={t} className="b-delta">
                      {A && <A size={12} />}
                      {minus(s.delta)}
                      {t !== 'neutral' && <span className="sr">{t === 'ok' ? ', good' : ', not good'}</span>}
                    </Pill>
                  )}
                  {s.note && <span className="b-stat-note">{s.note}</span>}
                </span>
              )}
            </div>
          )
        })}
      </div>
    </>
  )
}
