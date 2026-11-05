import { useId, useMemo, useState, type PointerEvent } from 'react'
import type { BlockOf } from '../../../api/blocks'
import { useBlocks } from '../context'
import { Head } from '../parts/Head'
import { useWidth } from '../parts/useWidth'
import { barPath, formats, model, pickTicks, spread, textWidth } from './scale'
import './chart.css'

type B = BlockOf<'chart'>

// Chart is a small hand-drawn SVG chart, line or bar, up to three series
// in the series tokens. Lines are labelled where they end instead of with
// a legend; bars carry their values. Hover or touch shows the values at a
// point. The picture is hidden from screen readers, who get the same
// numbers as a table.
export function Chart({ block: b }: { block: B }) {
  const ctx = useBlocks()
  const [ref, W] = useWidth<HTMLDivElement>(560)
  const [hover, setHover] = useState<number | null>(null)
  const id = useId()
  const m = useMemo(() => model(b, ctx.zone), [b, ctx.zone])
  const f = useMemo(() => formats(b, ctx.zone), [b, ctx.zone])
  const bar = b.style === 'bar'
  const H = ctx.placement === 'inline' ? 200 : 240
  const multi = m.series.length > 1

  // Room for the y labels on the left and the direct labels on the right.
  const padL = Math.max(...m.ticks.map((t) => textWidth(f.tick(t)))) + 12
  const ends = m.series.map((s) => {
    const last = s.pts[s.pts.length - 1]
    return { name: s.name, value: last ? f.full(last.y) : '', last }
  })
  const withValues = Math.max(...ends.map((e) => textWidth(multi ? `${e.name} ${e.value}` : e.value))) + 16
  const namesOnly = Math.max(...ends.map((e) => textWidth(e.name))) + 16
  const showValues = withValues <= W * 0.34
  const padR = bar ? 4 : Math.min(W * 0.34, showValues ? withValues : namesOnly)
  const padT = (b.marks?.length ? 20 : 8) + (bar ? 12 : 0)
  const padB = 24
  const x0 = padL
  const x1 = Math.max(x0 + 40, W - padR)
  const y0 = padT
  const y1 = H - padB
  const sy = (v: number) => y1 - ((v - m.lo) / (m.hi - m.lo || 1)) * (y1 - y0)

  // x: time is linear in time, categories are bands.
  const n = m.keys.length
  const band = (x1 - x0) / Math.max(1, n)
  const minX = m.xs[0] ?? 0
  const maxX = m.xs[n - 1] ?? 1
  const sx = (x: number) => {
    if (m.ordinal) return x0 + band * (x + 0.5)
    if (maxX === minX) return (x0 + x1) / 2
    return x0 + ((x - minX) / (maxX - minX)) * (x1 - x0)
  }
  const keyX = (i: number) => sx(m.xs[i])

  const onMove = (e: PointerEvent<SVGRectElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    const px = e.clientX - r.left + x0
    let best = 0
    for (let i = 1; i < n; i++) if (Math.abs(keyX(i) - px) < Math.abs(keyX(best) - px)) best = i
    setHover(best)
  }

  const labelYs = spread(
    ends.map((e) => (e.last ? sy(e.last.y) : y1)),
    15,
    y0 + 4,
    y1,
  )
  const xTicks = m.ordinal ? pickTicks(n, Math.max(2, Math.floor((x1 - x0) / 40))) : pickTicks(n, Math.max(2, Math.floor((x1 - x0) / 72)))
  const groupW = Math.min(band * 0.68, 28 * m.series.length + 8)
  const barW = Math.max(4, (groupW - (m.series.length - 1) * 2) / m.series.length)

  return (
    <>
      <Head block={b} />
      <figure className="b-surface b-chart" data-style={b.style}>
        <div ref={ref} className="b-chart-plot">
          <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} aria-hidden="true">
            {!bar && m.series.length === 1 && (
              <defs>
                <linearGradient id={`${id}-fill`} x1="0" x2="0" y1="0" y2="1">
                  <stop offset="0" className="b-fill-top" />
                  <stop offset="1" className="b-fill-bottom" />
                </linearGradient>
              </defs>
            )}
            {m.ticks.map((t) => (
              <g key={t} className="b-tick">
                <line x1={x0} x2={x1} y1={sy(t)} y2={sy(t)} />
                <text x={x0 - 8} y={sy(t)} dy="0.32em" textAnchor="end">
                  {f.tick(t)}
                </text>
              </g>
            ))}
            {xTicks.map((i) => {
              const x = keyX(i)
              const label = f.x(m.keys[i])
              const max = m.ordinal ? Math.max(3, Math.floor((band * Math.max(1, n / xTicks.length)) / 6.6)) : 12
              const anchor = m.ordinal ? 'middle' : i === 0 ? 'start' : i === n - 1 ? 'end' : 'middle'
              return (
                <text key={i} className="b-xlabel" x={anchor === 'start' ? Math.max(x, x0) : x} y={H - 6} textAnchor={anchor}>
                  {label.length > max ? `${label.slice(0, Math.max(1, max - 1))}…` : label}
                </text>
              )
            })}
            {b.marks?.map((mk, i) => {
              const k = m.keys.findIndex((key) => String(key) === String(mk.x))
              if (k < 0) return null
              const x = keyX(k)
              const anchor = x > x1 - 40 ? 'end' : x < x0 + 40 ? 'start' : 'middle'
              return (
                <g key={i} className="b-mark">
                  <line x1={x} x2={x} y1={y0 - 6} y2={y1} />
                  <text x={x} y={y0 - 10} textAnchor={anchor}>
                    {mk.label}
                  </text>
                </g>
              )
            })}
            {bar
              ? m.series.map((s, si) =>
                  s.pts.map((p) => {
                    const cx = sx(p.x)
                    const left = cx - groupW / 2 + si * (barW + 2)
                    const base = sy(0)
                    // A zero still shows, as a 1px bar on the axis.
                    const end = Math.abs(sy(p.y) - base) < 1 ? base - 1 : sy(p.y)
                    const top = Math.min(end, base)
                    return (
                      <g key={`${si}-${p.x}`} className={`b-bar s${si + 1}${hover !== null && hover !== p.x ? ' dim' : ''}`}>
                        <path d={barPath(left, barW, end, base, Math.min(4, barW / 4))} />
                        {m.series.length === 1 && (
                          <text x={left + barW / 2} y={top - 6} textAnchor="middle">
                            {f.full(p.y)}
                          </text>
                        )}
                      </g>
                    )
                  }),
                )
              : m.series.map((s, si) => {
                  const d = s.pts.map((p, i) => `${i ? 'L' : 'M'}${sx(p.x).toFixed(1)} ${sy(p.y).toFixed(1)}`).join(' ')
                  const last = s.pts[s.pts.length - 1]
                  return (
                    <g key={si} className={`b-line s${si + 1}`}>
                      {m.series.length === 1 && s.pts.length > 1 && <path className="b-area" d={`${d} L${sx(last.x).toFixed(1)} ${y1} L${sx(s.pts[0].x).toFixed(1)} ${y1} Z`} fill={`url(#${id}-fill)`} />}
                      <path d={d} />
                      {last && <circle className="b-end" cx={sx(last.x)} cy={sy(last.y)} r={3.5} />}
                    </g>
                  )
                })}
            {!bar &&
              ends.map(
                (e, si) =>
                  e.last && (
                    <text key={si} className={`b-endlabel s${si + 1}`} x={x1 + 8} y={labelYs[si]} dy="0.32em">
                      {multi && <tspan className="name">{e.name}</tspan>}
                      {showValues && <tspan className="value">{multi ? ` ${e.value}` : e.value}</tspan>}
                      {!showValues && !multi && <tspan className="value">{e.value}</tspan>}
                    </text>
                  ),
              )}
            {hover !== null && !bar && (
              <g className="b-hover">
                <line x1={keyX(hover)} x2={keyX(hover)} y1={y0} y2={y1} />
                {m.series.map((s, si) => {
                  const p = s.pts.find((q) => q.x === m.xs[hover])
                  return p ? <circle key={si} className={`s${si + 1}`} cx={sx(p.x)} cy={sy(p.y)} r={4} /> : null
                })}
              </g>
            )}
            <rect className="b-hit" x={x0} y={y0} width={Math.max(0, x1 - x0)} height={Math.max(0, y1 - y0)} onPointerMove={onMove} onPointerDown={onMove} onPointerLeave={() => setHover(null)} />
          </svg>
          {hover !== null && <Tip m={m} f={f} i={hover} x={keyX(hover)} W={W} />}
        </div>
        {multi && bar && (
          <div className="b-legend" aria-hidden="true">
            {m.series.map((s, si) => (
              <span key={si} className={`s${si + 1}`}>
                <i />
                {s.name}
              </span>
            ))}
          </div>
        )}
        <table className="sr">
          <caption>{b.title || m.series.map((s) => s.name).join(', ')}</caption>
          <thead>
            <tr>
              <th scope="col">{m.time ? 'Date' : 'Item'}</th>
              {m.series.map((s, si) => (
                <th key={si} scope="col">
                  {s.name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {m.keys.map((k, i) => (
              <tr key={i}>
                <th scope="row">{f.x(k)}</th>
                {m.series.map((s, si) => {
                  const p = s.pts.find((q) => q.x === m.xs[i])
                  return <td key={si}>{p ? f.full(p.y) : ''}</td>
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </figure>
    </>
  )
}

// Tip is the values at the hovered x, beside the cursor line.
function Tip({ m, f, i, x, W }: { m: ReturnType<typeof model>; f: ReturnType<typeof formats>; i: number; x: number; W: number }) {
  const right = x > W * 0.6
  return (
    <div className="b-tip" style={right ? { right: W - x + 12 } : { left: x + 12 }}>
      <span className="b-tip-x">{f.x(m.keys[i])}</span>
      {m.series.map((s, si) => {
        const p = s.pts.find((q) => q.x === m.xs[i])
        return p ? (
          <span key={si} className={`b-tip-row s${si + 1}`}>
            <i />
            {m.series.length > 1 && <span className="n">{s.name}</span>}
            <b>{f.full(p.y)}</b>
          </span>
        ) : null
      })}
    </div>
  )
}
