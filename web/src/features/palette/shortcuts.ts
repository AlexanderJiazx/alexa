// The app's keyboard shortcuts (WP-12, IX P8). The decision is a pure
// function of the key and where focus is, so it tests without a DOM.

export type ShortcutId = 'palette' | 'composer' | 'stop' | 'reflect' | 'panel' | 'settings' | 'escape'

export type KeyInfo = {
  key: string
  meta: boolean
  ctrl: boolean
  alt: boolean
  shift: boolean
  // typing is true while focus is in a text field, where plain keys type.
  typing: boolean
}

// isMac picks ⌘ over Ctrl for the modifier.
export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent)

// mod is the label of the modifier key, for hints: ⌘ on a Mac, Ctrl+ elsewhere.
export const mod = isMac ? '⌘' : 'Ctrl+'

// hints are the labels shown next to commands.
export const hints: Record<Exclude<ShortcutId, 'escape'>, string> = {
  palette: `${mod}K`,
  composer: '/',
  stop: `${mod}.`,
  reflect: `${mod}J`,
  panel: `${mod}B`,
  settings: `${mod},`,
}

// shortcut says which shortcut a key press is, or null. ⌘K, ⌘., ⌘J, ⌘B and
// ⌘, work everywhere, even while typing; / only when not typing, since it's a
// character too.
export function shortcut(k: KeyInfo, mac = isMac): ShortcutId | null {
  const modDown = mac ? k.meta && !k.ctrl : k.ctrl && !k.meta
  if (modDown && !k.alt && !k.shift) {
    const key = k.key.toLowerCase()
    if (key === 'k') return 'palette'
    if (key === '.') return 'stop'
    if (key === 'j') return 'reflect'
    if (key === 'b') return 'panel'
    if (key === ',') return 'settings'
    return null
  }
  if (k.meta || k.ctrl || k.alt) return null
  if (k.key === '/' && !k.typing) return 'composer'
  if (k.key === 'Escape') return 'escape'
  return null
}

// isTyping says whether an element takes typed text.
export function isTyping(el: Element | null): boolean {
  if (!el) return false
  const tag = el.tagName
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (tag === 'INPUT') return !/^(button|checkbox|radio|range|submit|reset|file|color)$/i.test((el as HTMLInputElement).type)
  return (el as HTMLElement).isContentEditable
}
