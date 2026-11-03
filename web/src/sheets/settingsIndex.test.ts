import { describe, expect, it } from 'vitest'
import { findSettings, sections, sectionsFor, settingsIndex } from './settingsIndex'

const anyone = { isAdmin: false, email: false }
const admin = { isAdmin: true, email: true }
const keys = (q: string, c = anyone) => findSettings(q, c).map((s) => s.key)

describe('sectionsFor', () => {
  it('lists the sections in order, Users for admins only', () => {
    expect(sectionsFor(anyone).map((s) => s.title)).toEqual(['General', 'Notifications', 'Memory', 'Logins', 'Scheduled', 'Conversation', 'Account'])
    expect(sectionsFor(admin).map((s) => s.id).at(-1)).toBe('users')
  })

  it('puts every setting in a section there is', () => {
    const ids = new Set(sections.map((s) => s.id))
    for (const s of settingsIndex) expect(ids.has(s.section)).toBe(true)
    expect(new Set(settingsIndex.map((s) => s.key)).size).toBe(settingsIndex.length)
  })
})

describe('findSettings', () => {
  it('finds nothing with nothing typed', () => {
    expect(findSettings('', anyone)).toEqual([])
    expect(findSettings('   ', anyone)).toEqual([])
  })

  it('finds a setting by its name, best first', () => {
    expect(keys('timezone')[0]).toBe('timezone')
    expect(keys('theme')[0]).toBe('theme')
    expect(keys('password')[0]).toBe('password')
    expect(keys('sign')[0]).toBe('signout')
  })

  it('finds one by what people call it', () => {
    expect(keys('dark mode')).toEqual(['theme'])
    expect(keys('dark')).toEqual(['theme'])
    expect(keys('forget')).toEqual(['memory'])
    expect(keys('time zone')[0]).toBe('timezone')
    expect(keys('log out')).toEqual(['signout'])
    expect(keys('summarize')).toEqual(['compact'])
  })

  it('finds a section’s settings by the section’s name', () => {
    expect(keys('account')).toEqual(expect.arrayContaining(['password', 'signout']))
    expect(keys('general')).toEqual(['name', 'timezone', 'about', 'theme'])
  })

  it('ignores case and accents', () => {
    expect(keys('THEME')).toEqual(['theme'])
    expect(keys('thème')).toEqual(['theme'])
  })

  it('leaves out what this person doesn’t see', () => {
    expect(keys('users')).toEqual([])
    expect(keys('users', admin)).toEqual(['users'])
    expect(keys('email')).toEqual([])
    expect(keys('email', admin)[0]).toBe('email')
  })

  it('doesn’t find by letters in order alone, and finds nothing for nonsense', () => {
    expect(keys('tme')).toEqual([])
    expect(keys('zebra')).toEqual([])
  })
})
