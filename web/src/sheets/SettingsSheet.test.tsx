import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it } from 'vitest'
import type { User } from '../api/types'
import { openSettings, openSheet } from '../state/actions'
import { initialUI, setApp, setUI, store } from '../state/store'
import { openSheetFor } from './intent'
import { overlayOf } from './registry'
import { SettingsInline } from './Settings'
import { followUser, GeneralPane, openAboutYou } from './SettingsSheet'

const user: User = { id: 1, username: 'sam', displayName: 'Sam', email: '', timezone: 'Europe/Lisbon', about: '', isAdmin: false, createdAt: 1 }
const ui = () => store.get().ui

afterEach(() => {
  setUI(initialUI)
  setApp({ user: null, config: null })
})

describe('opening Settings', () => {
  it('opens the sheets that are sections of it at that section', () => {
    openSheet('memory')
    expect(ui()).toMatchObject({ sheet: 'settings', section: 'memory' })
    openSheet('logins')
    expect(ui()).toMatchObject({ sheet: 'settings', section: 'logins' })
    openSheet('schedules')
    expect(ui()).toMatchObject({ sheet: 'settings', section: 'schedules' })
    openSheet('users')
    expect(ui()).toMatchObject({ sheet: 'settings', section: 'users' })
  })

  it('opens Settings itself at its first screen, and shuts the menu', () => {
    setUI({ menu: true, section: 'memory' })
    openSheet('settings')
    expect(ui()).toMatchObject({ sheet: 'settings', section: null, menu: false })
    openSettings('account')
    expect(ui().section).toBe('account')
  })

  it('keeps the Notifications inbox a sheet of its own', () => {
    openSheet('notifications')
    expect(ui().sheet).toBe('notifications')
    expect(overlayOf('notifications')).toBe('notifications')
    expect(overlayOf('memory')).toBe('settings')
    expect(overlayOf(null)).toBe(null)
  })

  it('opens a section for an intent (the palette’s New schedule)', () => {
    openSheetFor('schedules', 'new')
    expect(ui()).toMatchObject({ sheet: 'settings', section: 'schedules' })
  })
})

// D11: the home checklist's "Tell Alexa where you live" opens Settings to
// About you, and the field says what to write there. That the field takes
// the focus needs a browser (useFirstFocus), and was checked in one.
describe('Settings, opened to say where you live', () => {
  it('opens General', () => {
    openAboutYou()
    expect(ui()).toMatchObject({ sheet: 'settings', section: 'general' })
  })

  it('asks in your own words where you live', () => {
    setApp({ user })
    const out = renderToStaticMarkup(<GeneralPane />)
    expect(out).toContain('placeholder="I live in …, I work as …, I prefer …"')
  })
})

describe('the Settings window', () => {
  it('lists the sections down the side, General shown first on a wide screen', () => {
    setApp({ user })
    const out = renderToStaticMarkup(<SettingsInline />)
    const items = [...out.matchAll(/data-section="(\w+)"/g)].map((m) => m[1])
    expect(items).toEqual(['general', 'notifications', 'memory', 'logins', 'schedules', 'conversation', 'account'])
    expect(out).toMatch(/data-section="general" aria-current="page"/)
    expect(out).toContain('class="sr">Search settings</label>')
    expect(out).toContain('>Profile</h3>')
  })

  it('shows Users to admins only, and General to anyone else who asks for it', () => {
    setApp({ user })
    expect(renderToStaticMarkup(<SettingsInline initial="users" />)).toMatch(/data-section="general" aria-current="page"/)
    setApp({ user: { ...user, isAdmin: true } })
    const out = renderToStaticMarkup(<SettingsInline initial="users" />)
    expect(out).toMatch(/data-section="users" aria-current="page"/)
  })

  it('puts the Conversation’s Clear in a danger group at the foot', () => {
    setApp({ user })
    const out = renderToStaticMarkup(<SettingsInline initial="conversation" />)
    expect(out.indexOf('Summarise now')).toBeLessThan(out.indexOf('Clear conversation'))
    expect(out).toContain('class="group danger"')
  })

  it('replaces the sections with what a search finds, each with its section', () => {
    setApp({ user })
    const out = renderToStaticMarkup(<SettingsInline query="dark mode" />)
    expect(out).not.toContain('data-section=')
    expect(out).toContain('Theme</span><span class="settings-item-sub">General</span>')
  })

  it('says when nothing matches', () => {
    setApp({ user })
    expect(renderToStaticMarkup(<SettingsInline query="zebra" />)).toContain('No settings match “zebra”.')
  })
})

// FH-14: a change from another device reaches a form that isn't being
// edited, and doesn't undo one that is.
describe('followUser', () => {
  const was = { displayName: 'Sam', email: '', timezone: 'Europe/Lisbon', about: '' }

  it('takes every new value while nothing is edited', () => {
    const now = { ...was, displayName: 'Samuel', timezone: 'Europe/London' }
    expect(followUser(was, was, now)).toEqual(now)
  })

  it('keeps an edit, and takes the new values of the fields not being edited', () => {
    const editing = { ...was, about: 'I live in Porto' }
    const now = { ...was, displayName: 'Samuel', about: 'From the phone' }
    expect(followUser(editing, was, now)).toEqual({ ...was, displayName: 'Samuel', about: 'I live in Porto' })
  })
})
