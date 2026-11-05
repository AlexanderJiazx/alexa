import { describe, expect, it } from 'vitest'
import {
  compact,
  dateRange,
  dayDiff,
  daysBetween,
  fmtClock,
  fmtDate,
  fmtWhen,
  minus,
  money,
  moneyParts,
  moneyRuns,
  nights,
  numericCell,
  numericColumn,
  parseWhen,
  percentOff,
  relative,
  wallClock,
} from '../../lib/format'

// lib/format.ts's Intl helpers (WP-04), in a pinned locale so the
// expectations don't depend on the machine.
const en = 'en-US'
const sp = (s: string) => s.replace(/[\u202f\u2009\u00a0]/g, ' ')

describe('minus', () => {
  it('sets a leading hyphen as a minus sign, and nothing else', () => {
    expect(minus('-$38')).toBe('−$38')
    expect(minus('-2')).toBe('−2')
    expect(minus('+$180')).toBe('+$180')
    expect(minus('-')).toBe('-')
    expect(minus('well-known')).toBe('well-known')
  })
})

describe('money', () => {
  it('drops cents on whole amounts and keeps them otherwise', () => {
    expect(money(279, 'USD', en)).toBe('$279')
    expect(money(303.76, 'USD', en)).toBe('$303.76')
    expect(money(412, 'USD', en, true)).toBe('$412.00')
    expect(money(2340, 'USD', en)).toBe('$2,340')
  })

  it('writes the amount and the code when the code is not a currency', () => {
    expect(money(12, 'dollars', en)).toBe('12 dollars')
  })

  it('splits out the symbol wherever the locale puts it', () => {
    expect(moneyParts(279, 'USD', en)).toEqual({ symbol: '$', value: '279', symbolFirst: true })
    const de = moneyParts(418, 'EUR', 'de-DE')
    expect(de.symbol).toBe('€')
    expect(de.symbolFirst).toBe(false)
    expect(moneyRuns(-38, 'USD', en)).toEqual([
      { cur: false, text: '-' },
      { cur: true, text: '$' },
      { cur: false, text: '38' },
    ])
  })

  it('works out a discount', () => {
    expect(percentOff(279, 329)).toBe(15)
    expect(percentOff(279, 279)).toBe(0)
    expect(percentOff(279)).toBe(0)
  })

  it('shortens counts', () => {
    expect(compact(3120, en)).toBe('3.1K')
    expect(compact(940, en)).toBe('940')
    expect(compact(1_800_000, en)).toBe('1.8M')
  })
})

describe('numeric cells', () => {
  it('knows a number when it sees one', () => {
    for (const s of ['$279.00', '-$38', '1,234', '12%', '30 h', '4.6', '€418', '250 g', '(12)']) expect(numericCell(s), s).toBe(true)
    for (const s of ['Tomorrow', 'Nov 3', 'UA 1543', 'Best Buy', '']) expect(numericCell(s), s).toBe(false)
  })

  it('right-aligns a column when most of it is numbers', () => {
    expect(numericColumn(['$279.00', '$284.99', '—', '$279.00'])).toBe(true)
    expect(numericColumn(['Tomorrow', 'Saturday', '$4'])).toBe(false)
    expect(numericColumn(['', ''])).toBe(false)
  })
})

describe('dates', () => {
  it('keeps a wall-clock time as written, whatever the zone', () => {
    expect(fmtDate('2026-11-14', 'day', 'America/Los_Angeles', en)).toBe('Sat, Nov 14')
    expect(sp(fmtDate('2026-11-14T19:30', 'time', 'Asia/Tokyo', en))).toBe('7:30 PM')
    expect(parseWhen('2026-11-14')?.dateOnly).toBe(true)
  })

  it('shows an instant in the user’s zone', () => {
    expect(sp(fmtDate('2026-11-14T18:45-05:00', 'time', 'Europe/London', en))).toBe('11:45 PM')
    expect(sp(fmtDate('2026-11-14T18:45:00Z', 'time', 'America/New_York', en))).toBe('1:45 PM')
  })

  it('gives back what it can’t read', () => {
    expect(fmtDate('next Tuesday', 'day', undefined, en)).toBe('next Tuesday')
    expect(parseWhen('soon')).toBeNull()
  })

  it('reads a flight’s times on each airport’s clock', () => {
    const dep = wallClock('2026-11-14T18:45-05:00')!
    const arr = wallClock('2026-11-15T06:50+00:00')!
    expect(sp(fmtWhen(dep, 'time', en))).toBe('6:45 PM')
    expect(sp(fmtWhen(arr, 'time', en))).toBe('6:50 AM')
    expect(daysBetween(dep, arr)).toBe(1)
    expect(daysBetween(wallClock('2026-11-14T08:10')!, wallClock('2026-11-14T16:45')!)).toBe(0)
  })

  it('counts nights and days', () => {
    expect(nights('2026-11-14', '2026-11-17')).toBe(3)
    expect(nights('2026-11-30', '2026-12-02')).toBe(2)
    expect(nights('2026-11-17', '2026-11-14')).toBe(0)
    expect(dayDiff('2026-11-14', undefined)).toBe(0)
  })

  it('writes ranges and clocks the locale’s way', () => {
    expect(sp(dateRange('2026-11-14', '2026-11-17', undefined, en))).toMatch(/^Nov 14 ?– ?17$/)
    expect(sp(dateRange('2026-11-30', '2026-12-02', undefined, en))).toMatch(/^Nov 30 ?– ?Dec 2$/)
    expect(sp(fmtClock('08:10', en))).toBe('8:10 AM')
    expect(fmtClock('soon', en)).toBe('soon')
  })

  it('says how long ago or how soon', () => {
    const now = Date.UTC(2026, 9, 30, 12)
    expect(relative(now + 3 * 864e5, now, en)).toBe('in 3 days')
    expect(relative(now - 2 * 3600e3, now, en)).toBe('2 hours ago')
    expect(relative(now, now, en)).toBe('now')
  })
})
