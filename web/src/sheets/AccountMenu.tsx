import { useState, type ReactNode } from 'react'
import type { User } from '../api/types'
import { openPalette } from '../features/palette/Palette'
import { hints } from '../features/palette/shortcuts'
import { getTheme, setTheme, type Theme } from '../lib/theme'
import { closeMenu, openSheet, signOut } from '../state/actions'
import { useUnread } from '../state/selectors'
import type { SheetName } from '../state/store'
import { Bell, Contrast, Gear, Monitor, Moon, Out, Search, Sun } from '../ui/icons'
import { Menu, MenuItem, MenuSeparator } from '../ui/Menu'
import './sheets.css'

const themes: [Theme, string, ReactNode][] = [
  ['system', 'System', <Monitor key="s" size={16} />],
  ['light', 'Light', <Sun key="l" size={16} />],
  ['dark', 'Dark', <Moon key="d" size={16} />],
]

// The avatar opens the menu, and it's where focus goes back to when a sheet
// or Settings opened from here closes.
const focusAvatar = () => document.getElementById('account-button')?.focus({ preventScroll: true })

// AccountMenu is the menu under the avatar, grouped: who's signed in, the
// notifications first, then Settings (Memory, Logins, Scheduled and Users
// are its sections), the commands and the theme, and signing out on its own
// at the end (IX-F16).
export function AccountMenu({ user, leaving, onClose }: { user: User; leaving?: boolean; onClose: () => void }) {
  const unread = useUnread()
  const [theme, set] = useState(getTheme)
  const name = user.displayName || user.username
  const open = (s: SheetName) => () => {
    focusAvatar()
    openSheet(s)
  }
  return (
    <Menu id="account-menu" label="Account" onClose={onClose} leaving={leaving} className="account-menu">
      <div className="who">
        <span className="avatar lg" aria-hidden="true">
          {name.slice(0, 1).toUpperCase()}
        </span>
        <span className="who-text">
          <b>{name}</b>
          <span>@{user.username}</span>
        </span>
      </div>
      <MenuSeparator />
      <MenuItem icon={<Bell size={18} />} count={unread} onSelect={open('notifications')}>
        Notifications
      </MenuItem>
      <MenuSeparator />
      <MenuItem icon={<Gear size={18} />} hint={hints.settings} onSelect={open('settings')}>
        Settings
      </MenuItem>
      <MenuItem
        icon={<Search size={18} />}
        hint={hints.palette}
        onSelect={() => {
          focusAvatar()
          closeMenu()
          openPalette()
        }}
      >
        Commands
      </MenuItem>
      <div className="menu-theme" role="group" aria-label="Theme">
        <Contrast size={18} />
        <span className="menu-theme-label">Theme</span>
        {/* Drawn as a small Segmented (Settings' Theme), but each choice is
            a menu item, so the menu's keys move through them. */}
        <span className="seg sm menu-theme-seg">
          {themes.map(([t, label, icon]) => (
            <button
              key={t}
              type="button"
              role="menuitemradio"
              tabIndex={-1}
              aria-checked={theme === t}
              aria-label={`${label} theme`}
              title={label}
              onClick={() => {
                setTheme(t)
                set(t)
              }}
            >
              {icon}
            </button>
          ))}
        </span>
      </div>
      <MenuSeparator />
      <MenuItem icon={<Out size={18} />} tone="quiet" onSelect={signOut}>
        Sign out
      </MenuItem>
    </Menu>
  )
}
