import type { CSSProperties } from 'react'
import type { BlockOf } from '../../../api/blocks'
import { cx } from '../../../ui/cx'
import { Check, Sparkle } from '../../../ui/icons'
import { Pick } from '../cards/Card'
import { Head } from '../parts/Head'
import { Money } from '../parts/Money'
import '../table/table.css'
import './compare.css'

type B = BlockOf<'compare'>

// Compare is a side-by-side decision grid: options across, qualities down,
// the best of each row marked with a tick (never by colour alone), the
// pick's column called out, then Alexa's verdict. Below 440px of room it
// turns into one short card per option.
export function Compare({ block: b }: { block: B }) {
  const pick = b.items.findIndex((i) => i.pick)
  return (
    <>
      <Head block={b} meta={`${b.items.length} options`} />
      <div className="b-surface b-compare">
        <div className="b-cmp-wide b-scroll-x">
          <table className="b-grid" style={{ '--n': b.items.length } as CSSProperties}>
            <thead>
              <tr>
                <td className="b-corner" />
                {b.items.map((it, i) => (
                  <th key={i} scope="col" className={cx(i === pick && 'is-pick')}>
                    {i === pick && <Pick />}
                    <span className="b-cmp-name">{it.name}</span>
                    {it.price && <Money amount={it.price.amount} currency={it.price.currency} className="b-cmp-price" />}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {b.rows.map((r, j) => (
                <tr key={j}>
                  <th scope="row">{r.label}</th>
                  {b.items.map((_, i) => (
                    <Cell key={i} value={r.values[i]} best={r.best === i} pick={i === pick} />
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <ul className="b-cmp-narrow" role="list">
          {b.items.map((it, i) => (
            <li key={i} className={cx(i === pick && 'is-pick')}>
              <div className="b-cmp-item">
                <span className="b-cmp-name">{it.name}</span>
                {it.price && <Money amount={it.price.amount} currency={it.price.currency} className="b-cmp-price" />}
                {i === pick && <Pick />}
              </div>
              <dl>
                {b.rows.map((r, j) => (
                  <div key={j} className={cx(r.best === i && 'best')}>
                    <dt>{r.label}</dt>
                    <dd>
                      {r.best === i && <Check size={14} />}
                      {r.values[i] ?? '—'}
                      {r.best === i && <span className="sr"> (best)</span>}
                    </dd>
                  </div>
                ))}
              </dl>
            </li>
          ))}
        </ul>
        {b.verdict && (
          <p className="b-verdict">
            <Sparkle size={16} />
            <span>{b.verdict}</span>
          </p>
        )}
      </div>
    </>
  )
}

function Cell({ value, best, pick }: { value?: string; best: boolean; pick: boolean }) {
  return (
    <td className={cx(best && 'best', pick && 'is-pick')}>
      <span className="b-cell-v">
        {best && <Check size={14} />}
        {value ?? '—'}
        {best && <span className="sr"> (best)</span>}
      </span>
    </td>
  )
}
