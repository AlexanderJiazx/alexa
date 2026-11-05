import type { ReactNode } from 'react'
import { cx } from './cx'

export type Tone = 'neutral' | 'ink' | 'ok' | 'warn' | 'bad'

// Pill is a small status label: "Live", "Paused", "3 new". It isn't
// pressable; for that use Chip. pulse makes the dot breathe, for something
// happening now.
export function Pill({ tone = 'neutral', dot, pulse, className, children }: { tone?: Tone; dot?: boolean; pulse?: boolean; className?: string; children: ReactNode }) {
  return <span className={cx('pill', tone !== 'neutral' && tone, dot && 'dot', dot && pulse && 'pulse', className)}>{children}</span>
}
