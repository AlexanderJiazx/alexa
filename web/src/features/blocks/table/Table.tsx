import { useState } from 'react'
import type { BlockOf } from '../../../api/blocks'
import { numericColumn } from '../../../lib/format'
import { Chevron } from '../../../ui/icons'
import { useBlocks } from '../context'
import { Head } from '../parts/Head'
import './table.css'

type B = BlockOf<'table'>

// inline is how many rows show before "Show all".
export const inlineRows = 8

// Table is plain data: a header row, numbers right-aligned in tabular
// figures (detected when the model didn't say), 8 rows and then Show all,
// which grows the table in place. In the panel, where Expand brings it
// for the room, every row shows. Wide tables scroll sideways inside their
// frame, never the page.
export function Table({ block: b }: { block: B }) {
  const ctx = useBlocks()
  const [all, setAll] = useState(ctx.placement === 'panel')
  const cols = b.columns.length
  const numeric = b.columns.map((_, i) => i > 0 && numericColumn(b.rows.map((r) => r[i])))
  const align = b.columns.map((_, i) => b.align?.[i] ?? (numeric[i] ? 'right' : 'left'))
  const more = b.rows.length > inlineRows
  const rows = more && !all ? b.rows.slice(0, inlineRows) : b.rows
  return (
    <>
      <Head block={b} meta={`${b.rows.length} ${b.rows.length === 1 ? 'row' : 'rows'}`} />
      <div className="b-surface b-table">
        <div className="b-scroll-x">
          <table className="b-grid">
            <thead>
              <tr>
                {b.columns.map((c, i) => (
                  <th key={i} scope="col" className={`a-${align[i]}`}>
                    {c}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, j) => (
                <tr key={j}>
                  {Array.from({ length: cols }, (_, i) =>
                    i === 0 ? (
                      <th key={i} scope="row" className={`a-${align[i]}`}>
                        {r[i]}
                      </th>
                    ) : (
                      <td key={i} className={`a-${align[i]}${numeric[i] ? ' num' : ''}`}>
                        {r[i] ?? ''}
                      </td>
                    ),
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {more && (
          <button type="button" className="b-more" aria-expanded={all} onClick={() => setAll(!all)}>
            {all ? 'Show fewer' : `Show all ${b.rows.length} rows`}
            <Chevron size={16} />
          </button>
        )}
      </div>
    </>
  )
}
