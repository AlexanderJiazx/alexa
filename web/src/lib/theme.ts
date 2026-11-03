// The app follows the system's light or dark appearance unless the user
// picks one in Settings. The choice is kept on this device; index.html
// applies it before the first paint.

export type Theme = 'system' | 'light' | 'dark'

const key = 'alexa-theme'
// Guarded so the module imports where there's no matchMedia (tests).
const dark = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null

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
// match. The toolbar takes the page's own background, read back once the
// theme applies, so the colour lives only in the tokens.
export function applyTheme() {
  const t = getTheme()
  const root = document.documentElement
  if (t === 'system') delete root.dataset.theme
  else root.dataset.theme = t
  const bg = document.body ? getComputedStyle(document.body).backgroundColor : ''
  if (bg) document.querySelector('meta[name=theme-color]')?.setAttribute('content', bg)
}

dark?.addEventListener('change', applyTheme)
