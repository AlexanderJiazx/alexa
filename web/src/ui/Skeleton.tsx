import type { CSSProperties } from 'react'
import { cx } from './cx'

type Props = {
  width?: number | string
  height?: number | string
  // radius is a token name: xs, sm, md, lg or full.
  radius?: 'xs' | 'sm' | 'md' | 'lg' | 'full'
  // lines draws a paragraph of text lines, the last one shorter.
  lines?: number
  className?: string
}

const len = (v: number | string | undefined) => (typeof v === 'number' ? `${v}px` : v)

// Skeleton holds the place of something loading, with a slow shimmer (a
// fade when motion is reduced). Prefer it to a spinner when the shape of
// what's coming is known.
export function Skeleton({ width, height, radius = 'xs', lines, className }: Props) {
  if (lines)
    return (
      <span className={cx('skel-lines', className)} aria-hidden="true">
        {Array.from({ length: lines }, (_, i) => (
          <span key={i} className="skel line" style={i === lines - 1 && lines > 1 ? ({ '--w': '62%' } as CSSProperties) : undefined} />
        ))}
      </span>
    )
  return <span className={cx('skel', `r-${radius}`, className)} aria-hidden="true" style={{ '--w': len(width), '--h': len(height) } as CSSProperties} />
}
