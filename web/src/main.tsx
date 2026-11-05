import { StrictMode, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { applyTheme } from './lib/theme'
import './styles/tokens.css'
import './styles/base.css'
// After the global styles, so each feature's own stylesheet comes later.
import App from './app/App'

applyTheme()

const root = createRoot(document.getElementById('root')!)
const render = (node: ReactNode) => root.render(<StrictMode>{node}</StrictMode>)

// In development, ?gallery shows the dev gallery instead of the app: every
// feature's fixtures, with no sign-in and no socket. The production build
// leaves it out.
if (import.meta.env.DEV && new URLSearchParams(location.search).has('gallery')) {
  import('./dev/Gallery').then(({ Gallery }) => render(<Gallery />))
} else {
  render(<App />)
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('/sw.js').catch(() => {})
    })
  }
}
