import { useEffect, useRef, useState } from 'react'
import './gallery.css'
import type { GalleryPage } from './types'

// The dev gallery (?gallery, development only): every gallery.tsx in the
// app shows its fixtures here, in light and dark, at the widths the app
// lays out for. Each width is a real frame, so media queries apply as they
// would on a device of that size.

const modules = import.meta.glob<{ default: GalleryPage }>('../**/gallery.tsx', { eager: true })
const pages = Object.values(modules)
  .map((m) => m.default)
  .filter(Boolean)
  .sort((a, b) => a.title.localeCompare(b.title))

type Theme = 'system' | 'light' | 'dark'
const widths = [380, 680, 1280]
const themes: Theme[] = ['system', 'light', 'dark']

const params = () => new URLSearchParams(location.search)

function setTheme(t: Theme) {
  const root = document.documentElement
  if (t === 'system') delete root.dataset.theme
  else root.dataset.theme = t
}

export function Gallery() {
  const q = params()
  const page = pages.find((p) => p.name === q.get('gallery')) ?? pages[0]
  if (q.has('frame')) return <Frame page={page} theme={(q.get('theme') as Theme) || 'system'} />
  return <Shell page={page} />
}

// Shell is the gallery's own page: the toolbar and the frames.
function Shell({ page }: { page?: GalleryPage }) {
  const q = params()
  const [theme, setT] = useState<Theme>((q.get('theme') as Theme) || 'system')
  const [width, setWidth] = useState<number | 'all'>(Number(q.get('w')) || 'all')
  useEffect(() => {
    setTheme(theme)
    const n = params()
    n.set('theme', theme)
    if (width === 'all') n.delete('w')
    else n.set('w', String(width))
    history.replaceState(null, '', `?${n}`)
  }, [theme, width])
  const go = (name: string) => {
    const n = params()
    n.set('gallery', name)
    location.search = `?${n}`
  }
  return (
    <div className="gallery">
      <header className="g-bar">
        <h1>Gallery</h1>
        <div className="seg" role="radiogroup" aria-label="Page">
          {pages.map((p) => (
            <button key={p.name} type="button" role="radio" aria-checked={p.name === page?.name} onClick={() => go(p.name)}>
              {p.title}
            </button>
          ))}
        </div>
        <div className="g-end">
          <div className="seg" role="radiogroup" aria-label="Theme">
            {themes.map((t) => (
              <button key={t} type="button" role="radio" aria-checked={t === theme} onClick={() => setT(t)}>
                {t[0].toUpperCase() + t.slice(1)}
              </button>
            ))}
          </div>
          <div className="seg" role="radiogroup" aria-label="Width">
            {(['all', ...widths] as const).map((w) => (
              <button key={w} type="button" role="radio" aria-checked={w === width} onClick={() => setWidth(w)}>
                {w === 'all' ? 'All' : w}
              </button>
            ))}
          </div>
        </div>
      </header>
      {page ? (
        <div className="g-frames">
          {(width === 'all' ? widths : [width]).map((w) => (
            <FrameView key={`${page.name}-${w}-${theme}`} page={page} width={w} theme={theme} />
          ))}
        </div>
      ) : (
        <p className="g-none">No gallery pages yet. Add a gallery.tsx next to a feature.</p>
      )}
    </div>
  )
}

// FrameView is one width of the page, in a frame that grows to fit it.
function FrameView({ page, width, theme }: { page: GalleryPage; width: number; theme: Theme }) {
  const ref = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState(480)
  useEffect(() => {
    const on = (e: MessageEvent) => {
      if (e.source === ref.current?.contentWindow && e.data?.type === 'gallery:height') setHeight(e.data.height)
    }
    window.addEventListener('message', on)
    return () => window.removeEventListener('message', on)
  }, [])
  const src = `?gallery=${encodeURIComponent(page.name)}&frame=1&theme=${theme}`
  return (
    <figure className="g-frame">
      <figcaption>{width}</figcaption>
      <iframe ref={ref} title={`${page.title} at ${width}px`} src={src} width={width} height={height} />
    </figure>
  )
}

// Frame is what renders inside a frame: the page alone, telling the gallery
// how tall it is.
function Frame({ page, theme }: { page?: GalleryPage; theme: Theme }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    setTheme(theme)
    const el = ref.current
    if (!el) return
    const post = () => parent.postMessage({ type: 'gallery:height', height: Math.ceil(el.getBoundingClientRect().height) }, location.origin)
    const ro = new ResizeObserver(post)
    ro.observe(el)
    post()
    return () => ro.disconnect()
  }, [theme])
  return (
    <div ref={ref} className="g-page">
      {page?.render()}
    </div>
  )
}
