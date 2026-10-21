// The app follows the system's light or dark appearance unless the user
// picks one in Settings. The choice is kept on this device; index.html
// applies it before the first paint.

export type Theme = 'system' | 'light' | 'dark'

const key = 'alexa-theme'
const dark = matchMedia('(prefers-color-scheme: dark)')

export function getTheme(): Theme {
  try {
    const t = localStorage.getItem(key)
    if (t === 'light' || t === 'dark') return t
  } catch {
    /* storage unavailable */
  }
  return 'system'
}

export function setTheme(t: Theme) {
  try {
    if (t === 'system') localStorage.removeItem(key)
    else localStorage.setItem(key, t)
  } catch {
    /* storage unavailable */
  }
  applyTheme()
}

// applyTheme sets the page's theme and the browser's toolbar colour to
// match.
export function applyTheme() {
  const t = getTheme()
  const root = document.documentElement
  if (t === 'system') delete root.dataset.theme
  else root.dataset.theme = t
  const isDark = t === 'dark' || (t === 'system' && dark.matches)
  document.querySelector('meta[name=theme-color]')?.setAttribute('content', isDark ? '#121211' : '#F6F6F4')
}

dark.addEventListener('change', applyTheme)
