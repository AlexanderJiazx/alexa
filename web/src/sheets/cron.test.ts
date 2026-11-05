import { describe, expect, it } from 'vitest'
import { describeCron, fmtRun, fromCron, inSentence, nameFrom, namedByPrompt, toCron, zoneNote } from './cron'

describe('schedule builder', () => {
  it('reads the common shapes', () => {
    expect(fromCron('0 8 * * *')).toMatchObject({ freq: 'daily', time: '08:00' })
    expect(fromCron('30 7 * * 1-5')).toMatchObject({ freq: 'weekdays', time: '07:30' })
    expect(fromCron('15 18 * * 5')).toMatchObject({ freq: 'weekly', time: '18:15', day: 5 })
    expect(fromCron('0 9 * * 7')).toMatchObject({ freq: 'weekly', day: 0 })
  })

  it('keeps anything else as custom cron', () => {
    expect(fromCron('*/15 * * * *')).toMatchObject({ freq: 'custom', cron: '*/15 * * * *' })
    expect(fromCron('0 9 1 * *')).toMatchObject({ freq: 'custom' })
    expect(fromCron('nonsense')).toMatchObject({ freq: 'custom', cron: 'nonsense' })
  })

  it('writes cron back, round trip', () => {
    for (const c of ['0 8 * * *', '30 7 * * 1-5', '15 18 * * 5']) expect(toCron(fromCron(c))).toBe(c)
    expect(toCron({ freq: 'weekly', time: '09:05', day: 2, cron: '' })).toBe('5 9 * * 2')
    expect(toCron({ freq: 'custom', time: '', day: 0, cron: ' */5 * * * * ' })).toBe('*/5 * * * *')
  })

  it('says it in words', () => {
    expect(describeCron('30 7 * * 1-5')).toMatch(/^Weekdays at 7:30/)
    expect(describeCron('0 8 * * *')).toMatch(/^Every day at 8:00/)
    expect(describeCron('*/15 * * * *')).toBe('Every 15 minutes')
  })
})

describe('run times', () => {
  // Fri 30 Oct 2026, 10:00 in Toronto (14:00 UTC). Toronto leaves summer
  // time on Sunday 1 November.
  const now = Date.UTC(2026, 9, 30, 14, 0)
  const at = (h: number, days = 0) => Date.UTC(2026, 9, 30 + days, h, 0)
  const sp = (s: string) => s.replace(/\s/g, ' ')

  it('says them in the user’s timezone, not the device’s', () => {
    // 15:00 UTC is 8:00 in Los Angeles and 11:00 in Toronto.
    expect(sp(fmtRun(at(15), 'America/Los_Angeles', now))).toBe('Today at 8:00 AM')
    expect(sp(fmtRun(at(15), 'America/Toronto', now))).toBe('Today at 11:00 AM')
  })

  it('names the day: today, tomorrow, this week, then the date', () => {
    const tz = 'America/Toronto'
    expect(sp(fmtRun(at(13, 1), tz, now))).toBe('Tomorrow at 9:00 AM')
    expect(sp(fmtRun(at(14, 2), tz, now))).toBe('Sunday at 9:00 AM')
    expect(sp(fmtRun(at(14, 9), tz, now))).toBe('Sun, Nov 8 at 9:00 AM')
    // Late at night in Toronto is already tomorrow in UTC.
    expect(sp(fmtRun(Date.UTC(2026, 9, 31, 3, 30), tz, now))).toBe('Today at 11:30 PM')
  })

  it('falls back to this device for a timezone it doesn’t know', () => {
    expect(fmtRun(at(15), 'Not/AZone', now)).toMatch(/ at /)
    expect(fmtRun(undefined, 'America/Toronto', now)).toBe('')
  })

  it('names the user’s timezone only when the device is elsewhere', () => {
    expect(zoneNote('America/Los_Angeles', 'America/Toronto')).toBe('Los Angeles time')
    expect(zoneNote('America/Toronto', 'America/Toronto')).toBe('')
    expect(zoneNote('', 'America/Toronto')).toBe('')
    expect(zoneNote('Not/AZone', 'America/Toronto')).toBe('')
  })

  it('reads in a sentence', () => {
    expect(inSentence('Tomorrow at 9:00 AM')).toBe('tomorrow at 9:00 AM')
    expect(inSentence('Sunday at 9:00 AM')).toBe('Sunday at 9:00 AM')
  })
})

describe('schedule names', () => {
  const prompt = 'Check Hacker News and send me the top five stories with a line on each'

  it('takes a missing name from the prompt, cut at a word', () => {
    expect(nameFrom(prompt)).toBe('Check Hacker News and send me the top…')
    expect(nameFrom('Morning briefing')).toBe('Morning briefing')
  })

  it('knows a name that only repeats the prompt', () => {
    expect(namedByPrompt(nameFrom(prompt), prompt)).toBe(true)
    // As older versions named them: the first 40 characters.
    expect(namedByPrompt(prompt.slice(0, 40), prompt)).toBe(true)
    expect(namedByPrompt('Morning briefing', prompt)).toBe(false)
    expect(namedByPrompt('', prompt)).toBe(true)
  })
})
