import type { ComponentType } from 'react'
import type { SheetName } from '../state/store'
import { NotificationsSheet } from './NotificationsSheet'
import { Settings } from './Settings'

// open is false while the overlay plays its exit; onClose asks it to close.
export type SheetProps = { open: boolean; onClose: () => void }

// What ui.sheet opens over the app: Settings, where Memory, Logins,
// Scheduled and Users are sections (openSheet), and the Notifications inbox
// on its own. Each reads what it shows from the store or the API.
export const sheets = { settings: Settings, notifications: NotificationsSheet } satisfies Record<string, ComponentType<SheetProps>>

// overlayOf is which of them a sheet name shows.
export const overlayOf = (s: SheetName | null): keyof typeof sheets | null => (s === null ? null : s === 'notifications' ? 'notifications' : 'settings')
