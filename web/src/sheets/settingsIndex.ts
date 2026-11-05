import { score } from '../features/palette/filter'
import type { SettingsSection } from '../state/store'

// What Settings has, for its side and its search. Pure, so it's tested
// without a DOM.

export type SectionInfo = { id: SettingsSection; title: string; admin?: boolean }

// The sections, in the order the side lists them.
export const sections: readonly SectionInfo[] = [
  { id: 'general', title: 'General' },
  { id: 'notifications', title: 'Notifications' },
  { id: 'memory', title: 'Memory' },
  { id: 'logins', title: 'Logins' },
  { id: 'schedules', title: 'Scheduled' },
  { id: 'conversation', title: 'Conversation' },
  { id: 'account', title: 'Account' },
  { id: 'users', title: 'Users', admin: true },
]

export const sectionTitle = (id: SettingsSection) => sections.find((s) => s.id === id)?.title ?? ''

// A setting the search finds. key is also the end of the id of the row or
// field it goes to (useSettingId); a whole section (Memory, Logins) has no
// row of its own, and choosing it goes to the top of the section.
export type Setting = {
  key: string
  title: string
  section: SettingsSection
  keywords?: string
  // admin: only for admins. email: only while the email field shows (the
  // server sends email, or an address is saved already).
  admin?: boolean
  email?: boolean
}

export const settingsIndex: readonly Setting[] = [
  { key: 'name', title: 'Name', section: 'general', keywords: 'profile display name what to call me' },
  { key: 'timezone', title: 'Timezone', section: 'general', keywords: 'time zone clock city where I live location' },
  { key: 'email', title: 'Email for notifications', section: 'general', keywords: 'mail address profile', email: true },
  { key: 'about', title: 'About you', section: 'general', keywords: 'profile bio where I live preferences instructions context' },
  { key: 'theme', title: 'Theme', section: 'general', keywords: 'appearance dark mode light mode system colour color' },
  { key: 'push', title: 'Notifications on this device', section: 'notifications', keywords: 'push alerts notify phone browser' },
  { key: 'memory', title: 'Memory', section: 'memory', keywords: 'remember forget facts about me memories' },
  { key: 'logins', title: 'Saved logins', section: 'logins', keywords: 'passwords sites accounts sign in credentials' },
  { key: 'schedules', title: 'Scheduled', section: 'schedules', keywords: 'schedules recurring every daily cron jobs reminders' },
  { key: 'compact', title: 'Summarise now', section: 'conversation', keywords: 'summarize compact shorten context tokens long' },
  { key: 'reset', title: 'Clear conversation', section: 'conversation', keywords: 'reset wipe start over erase history danger' },
  { key: 'password', title: 'Change password', section: 'account', keywords: 'password security' },
  { key: 'signout', title: 'Sign out', section: 'account', keywords: 'log out logout leave' },
  { key: 'users', title: 'Users', section: 'users', keywords: 'people admin add someone reset password', admin: true },
]

export type SettingsContext = { isAdmin: boolean; email: boolean }

const allowed = (c: SettingsContext) => (x: { admin?: boolean; email?: boolean }) => (!x.admin || c.isAdmin) && (!x.email || c.email)

// sectionsFor is the sections this person sees: Users is for admins.
export const sectionsFor = (c: SettingsContext) => sections.filter(allowed(c))

// The palette's score for letters in order and nothing better.
const lettersInOrder = 10

// findSettings is what a search shows: the settings that match, best first,
// with equal matches in Settings' own order. A setting matches by its
// title, its keywords or its section's name, as the palette ranks commands
// (features/palette/filter.ts), but not by letters in order alone: here
// that would find too much. Nothing typed finds nothing (the side shows
// the sections then).
export function findSettings(query: string, c: SettingsContext): Setting[] {
  if (!query.trim()) return []
  return settingsIndex
    .filter(allowed(c))
    .map((s, i) => ({ s, i, n: score({ title: s.title, keywords: `${s.keywords ?? ''} ${sectionTitle(s.section)}` }, query) }))
    .filter((x) => x.n > lettersInOrder)
    .sort((a, b) => b.n - a.n || a.i - b.i)
    .map((x) => x.s)
}
