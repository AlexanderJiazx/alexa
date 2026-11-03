import { useSyncExternalStore } from 'react'

// One clock for every ticking thing on screen, so ten timers cost one
// interval, and they all tick together.
const subs = new Set<() => void>()
let now = Date.now()
let timer: ReturnType<typeof setInterval> | null = null

function subscribe(fn: () => void) {
  subs.add(fn)
  if (!timer) {
    now = Date.now()
    timer = setInterval(() => {
      now = Date.now()
      subs.forEach((f) => f())
    }, 1000)
  }
  return () => {
    subs.delete(fn)
    if (!subs.size && timer) {
      clearInterval(timer)
      timer = null
    }
  }
}

// useNow is the current time, updated every second while mounted.
export function useNow() {
  return useSyncExternalStore(
    subscribe,
    () => now,
    () => now,
  )
}

// fmtDuration reads like speech, not a stopwatch: "8s", "1m 12s", "2m",
// "1h 5m".
export function fmtDuration(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return s % 60 ? `${m}m ${s % 60}s` : `${m}m`
  const h = Math.floor(m / 60)
  return m % 60 ? `${h}h ${m % 60}m` : `${h}h`
}

// Timer shows the time since from (until to, once it's over), ticking while
// it runs, in tabular figures so it doesn't jitter.
export function Timer({ from, to, className }: { from: number; to?: number; className?: string }) {
  return to ? <Static from={from} to={to} className={className} /> : <Ticking from={from} className={className} />
}

function Ticking({ from, className }: { from: number; className?: string }) {
  const t = useNow()
  return <Static from={from} to={t} className={className} />
}

function Static({ from, to, className }: { from: number; to: number; className?: string }) {
  return (
    <time className={className ? `timer ${className}` : 'timer'} dateTime={`PT${Math.max(0, Math.floor((to - from) / 1000))}S`}>
      {fmtDuration(to - from)}
    </time>
  )
}
