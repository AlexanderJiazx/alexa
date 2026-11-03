import type { ReactNode } from 'react'
import type { Report, SurfaceKind } from '../../api/types'
import { prefillComposer } from '../../composer/focus'
import { getTheme, setTheme } from '../../lib/theme'
import { openSheetFor } from '../../sheets/intent'
import { closePanel, closeReflect, handBack, home, openReflect, openReport, openSheet, show, signOut, stop } from '../../state/actions'
import { Bell, Brain, Clock, Close, Folder, Gear, Globe, HandBack, History, KeyIcon, Moon, Out, People, Plus, Report as ReportIcon, Sparkle, Stop, Sun } from '../../ui/icons'
import { hints } from './shortcuts'

// The command palette's commands (WP-12, IX P8). Each one changes what's on
// screen or does what a button already does (Stop, Hand back); none of them
// makes a hidden turn.

export type Group = 'Now' | 'Go to' | 'Make' | 'Reports' | 'App'

export type Command = {
  id: string
  title: string
  group: Group
  keywords?: string
  hint?: string
  icon: ReactNode
  run: () => void
}

// What the list depends on, read from the store when the palette opens.
export type CommandContext = {
  status: 'idle' | 'working' | 'needs'
  control: boolean
  surface: SurfaceKind
  reflect: boolean
  isAdmin: boolean
  reports: Report[]
}

const dark = () => {
  const t = getTheme()
  return t === 'dark' || (t === 'system' && typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches)
}

// askAlexa is the last row of a search: what was typed, put in the
// composer for the user to send (or change) themselves. A request typed
// here isn't a dead end, and nothing is sent from the palette.
export const askAlexa = (query: string): Command => {
  const q = query.trim()
  return { id: 'ask', title: `Ask Alexa “${q}”`, group: 'Now', icon: <Sparkle size={18} />, run: () => prefillComposer(q) }
}

// withAsk is a search's results with that row at the end.
export const withAsk = (found: Command[], query: string) => (query.trim() ? [...found, askAlexa(query)] : found)

// The order here is the order with nothing typed.
export function buildCommands(c: CommandContext): Command[] {
  const list: Command[] = []
  const add = (cmd: Command | false) => cmd && list.push(cmd)
  const busy = c.status !== 'idle'

  add(busy && { id: 'stop', title: 'Stop', group: 'Now', keywords: 'cancel halt abort', hint: hints.stop, icon: <Stop size={18} />, run: stop })
  add(c.control && { id: 'handback', title: 'Hand back to Alexa', group: 'Now', keywords: 'browser control done', icon: <HandBack size={18} />, run: () => handBack() })
  add(
    c.surface
      ? { id: 'panel', title: 'Close the panel', group: 'Now', keywords: 'hide browser report', hint: hints.panel, icon: <Close size={18} />, run: closePanel }
      : { id: 'panel', title: 'Show the browser', group: 'Now', keywords: 'live screen panel web', hint: hints.panel, icon: <Globe size={18} />, run: () => show({ kind: 'browser' }) },
  )

  add(
    c.reflect
      ? { id: 'reflect', title: 'Back to Alexa', group: 'Go to', keywords: 'close reflect home', hint: hints.reflect, icon: <History size={18} />, run: closeReflect }
      : { id: 'reflect', title: 'Open Reflect', group: 'Go to', keywords: 'history conversation past search', hint: hints.reflect, icon: <History size={18} />, run: () => openReflect('conversation') },
  )
  add({ id: 'reports', title: 'Reports', group: 'Go to', keywords: 'documents reflect', icon: <ReportIcon size={18} />, run: () => openReflect('reports') })
  add({ id: 'files', title: 'Files', group: 'Go to', keywords: 'workspace uploads downloads', icon: <Folder size={18} />, run: () => openReflect('files') })
  add({ id: 'notifications', title: 'Notifications', group: 'Go to', keywords: 'inbox alerts', icon: <Bell size={18} />, run: () => openSheet('notifications') })
  add({ id: 'schedules', title: 'Scheduled', group: 'Go to', keywords: 'schedules jobs cron recurring', icon: <Clock size={18} />, run: () => openSheet('schedules') })
  add({ id: 'memory', title: 'Memory', group: 'Go to', keywords: 'remember facts about me', icon: <Brain size={18} />, run: () => openSheet('memory') })
  add({ id: 'logins', title: 'Logins', group: 'Go to', keywords: 'passwords accounts sign in', icon: <KeyIcon size={18} />, run: () => openSheet('logins') })
  add({ id: 'settings', title: 'Settings', group: 'Go to', keywords: 'preferences profile account', hint: hints.settings, icon: <Gear size={18} />, run: () => openSheet('settings') })
  add(c.isAdmin && { id: 'users', title: 'Users', group: 'Go to', keywords: 'people admin accounts', icon: <People size={18} />, run: () => openSheet('users') })

  add({ id: 'new-schedule', title: 'New schedule', group: 'Make', keywords: 'every daily recurring cron add', icon: <Plus size={18} />, run: () => openSheetFor('schedules', 'new') })
  add({ id: 'new-memory', title: 'Remember something', group: 'Make', keywords: 'memory add fact', icon: <Plus size={18} />, run: () => openSheetFor('memory', 'new') })
  add({ id: 'new-login', title: 'Add a login', group: 'Make', keywords: 'password account save', icon: <Plus size={18} />, run: () => openSheetFor('logins', 'new') })

  for (const r of c.reports.slice(0, 8))
    add({ id: `report:${r.id}`, title: r.title, group: 'Reports', keywords: 'report open', icon: <ReportIcon size={18} />, run: () => openReport(r.id) })

  const isDark = dark()
  add({
    id: 'theme',
    title: isDark ? 'Switch to light' : 'Switch to dark',
    group: 'App',
    keywords: 'theme appearance mode dark light toggle',
    icon: isDark ? <Sun size={18} /> : <Moon size={18} />,
    run: () => setTheme(isDark ? 'light' : 'dark'),
  })
  add({ id: 'home', title: 'Put this exchange away', group: 'App', keywords: 'home clear done', icon: <History size={18} />, run: home })
  add({ id: 'signout', title: 'Sign out', group: 'App', keywords: 'log out logout leave', icon: <Out size={18} />, run: signOut })
  return list
}
