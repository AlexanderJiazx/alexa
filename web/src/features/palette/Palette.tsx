import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { api } from '../../api/client'
import type { Report } from '../../api/types'
import { focusComposer } from '../../composer/focus'
import { closeMenu, closePanel, closeReflect, openReflect, openSettings, show, stop } from '../../state/actions'
import { createStore, store, useApp, useSelector } from '../../state/store'
import { cx } from '../../ui/cx'
import { Search } from '../../ui/icons'
import { Kbd } from '../../ui/Kbd'
import { useModal } from '../../ui/useModal'
import { buildCommands, withAsk, type Command, type Group } from './commands'
import { filterCommands } from './filter'
import './palette.css'
import { isTyping, shortcut } from './shortcuts'

// The palette's open state: a tiny store, so the account menu can open it.
const palette = createStore(false)
export const openPalette = () => palette.set(true)
export const closePalette = () => palette.set(false)
const togglePalette = () => palette.set((o) => !o)

// A modal (a sheet, a dialog, the palette, Reflect) is open: the main
// screen's shortcuts wait.
const modalOpen = () => !!document.querySelector('dialog:modal')

// Palette is the ⌘K command palette, and the keeper of the app's shortcuts:
// / focuses the composer, ⌘. stops, ⌘J opens Reflect, ⌘B the panel, ⌘,
// Settings and Esc closes the menu or else the panel (sheets and dialogs
// close themselves on Esc).
export function Palette() {
  const open = useSelector(palette, (o) => o)
  useShortcuts()
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  // Reports, fetched each time it opens (the list is short).
  const [reports, setReports] = useState<Report[]>([])
  const pending = useRef<Command | null>(null)
  const ctx = useApp(
    (s) => ({ status: s.data.session.status, control: s.data.session.control, surface: s.data.session.surface.kind, reflect: s.ui.reflect, isAdmin: !!s.user?.isAdmin }),
    (a, b) => a.status === b.status && a.control === b.control && a.surface === b.surface && a.reflect === b.reflect && a.isAdmin === b.isAdmin,
  )
  const m = useModal(open, closePalette, 120)

  useEffect(() => {
    if (!open) return
    setQuery('')
    setActive(0)
    api
      .reports()
      .then(setReports)
      .catch(() => {})
  }, [open])

  // A command runs once the palette has gone and focus is back where it
  // was, so a sheet it opens hands focus back there too when it closes.
  useEffect(() => {
    if (m.shown || !pending.current) return
    const run = pending.current
    pending.current = null
    run.run()
  }, [m.shown])

  // Rebuilt on each open too, so the theme command reads the theme as it is.
  const all = useMemo(() => buildCommands({ ...ctx, reports }), [ctx, reports, open])
  const shown = useMemo(() => withAsk(filterCommands(all, query), query), [all, query])
  const at = Math.min(active, Math.max(0, shown.length - 1))

  if (!m.shown) return null

  const run = (c: Command) => {
    pending.current = c
    closePalette()
  }

  return (
    <dialog ref={m.ref} className={cx('palette', m.leaving && 'leaving')} aria-label="Commands" onKeyDown={m.onKeyDown} onClick={m.onClick}>
      <PaletteBody
        commands={shown}
        query={query}
        active={at}
        onQuery={(q) => {
          setQuery(q)
          setActive(0)
        }}
        onActive={setActive}
        onRun={run}
      />
    </dialog>
  )
}

type BodyProps = {
  commands: Command[]
  query: string
  active: number
  onQuery: (q: string) => void
  onActive: (i: number) => void
  onRun: (c: Command) => void
}

// PaletteBody is the search field and the list, as drawn (the gallery
// draws it in place). The field keeps the focus; the arrows move the
// highlighted command and Enter runs it.
export function PaletteBody({ commands, query, active, onQuery, onActive, onRun }: BodyProps) {
  const list = useRef<HTMLDivElement>(null)
  const listId = useId()
  // keyed is set while the keyboard drives the highlight (it starts set,
  // where there's a keyboard: Enter runs the first command), and cleared
  // once the mouse moves it.
  const [keyed, setKeyed] = useState(() => typeof matchMedia !== 'function' || !matchMedia('(pointer: coarse)').matches)
  // Keeps the highlighted command in view by scrolling the list alone:
  // scrollIntoView would scroll the page behind it too.
  useEffect(() => {
    const l = list.current
    const el = l?.querySelector<HTMLElement>('[aria-selected=true]')
    if (!l || !el) return
    const box = l.getBoundingClientRect()
    const r = el.getBoundingClientRect()
    if (r.top < box.top) l.scrollTop -= box.top - r.top
    else if (r.bottom > box.bottom) l.scrollTop += r.bottom - box.bottom
  }, [active, commands])

  const onKey = (e: KeyboardEvent) => {
    setKeyed(true)
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (!commands.length) return
      onActive((active + (e.key === 'ArrowDown' ? 1 : -1) + commands.length) % commands.length)
    } else if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
      e.preventDefault()
      if (commands[active]) onRun(commands[active])
    }
  }

  // With nothing typed, commands sit under their group; a search is one
  // ranked list.
  const grouped = !query.trim()
  const rows: ({ group: Group } | { cmd: Command; i: number })[] = []
  commands.forEach((cmd, i) => {
    if (grouped && (i === 0 || commands[i - 1].group !== cmd.group)) rows.push({ group: cmd.group })
    rows.push({ cmd, i })
  })

  return (
    <>
      <div className="palette-search">
        <Search size={18} />
        <input
          autoFocus
          role="combobox"
          aria-expanded="true"
          aria-controls={listId}
          aria-activedescendant={commands[active] ? `${listId}-${active}` : undefined}
          aria-label="Search commands"
          placeholder="Search commands and reports"
          autoComplete="off"
          spellCheck={false}
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          onKeyDown={onKey}
        />
        <Kbd>esc</Kbd>
      </div>
      <div ref={list} className={keyed ? 'palette-list keyed' : 'palette-list'} role="listbox" id={listId} aria-label="Commands">
        {rows.map((r) =>
          'group' in r ? (
            <div key={`g-${r.group}`} className="palette-group" role="presentation">
              {r.group}
            </div>
          ) : (
            <div
              key={r.cmd.id}
              id={`${listId}-${r.i}`}
              role="option"
              aria-selected={r.i === active}
              className="palette-item"
              onMouseMove={() => {
                setKeyed(false)
                if (r.i !== active) onActive(r.i)
              }}
              onClick={() => onRun(r.cmd)}
            >
              <span className="palette-ic">{r.cmd.icon}</span>
              <span className="palette-title">{r.cmd.title}</span>
              {r.cmd.hint && <Kbd>{r.cmd.hint}</Kbd>}
            </div>
          ),
        )}
        {query.trim() && commands.every((c) => c.id === 'ask') && <p className="palette-empty">No commands or reports match.</p>}
      </div>
    </>
  )
}

// useShortcuts listens for the app's shortcuts while the app is on screen.
function useShortcuts() {
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing) return
      const id = shortcut({ key: e.key, meta: e.metaKey, ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey, typing: isTyping(document.activeElement) })
      if (!id) return
      const s = store.get()
      switch (id) {
        case 'palette':
          e.preventDefault()
          togglePalette()
          return
        case 'stop':
          e.preventDefault()
          if (s.data.session.status !== 'idle') stop()
          return
        case 'reflect':
          e.preventDefault()
          if (palette.get()) return
          if (s.ui.reflect) closeReflect()
          else openReflect('conversation')
          return
        case 'panel':
          e.preventDefault()
          if (modalOpen()) return
          if (s.data.session.surface.kind) closePanel()
          else show({ kind: 'browser' })
          return
        case 'settings':
          e.preventDefault()
          // Not over the palette or a question being asked (a dialog), and
          // Settings already open stays as it is.
          if (palette.get() || s.ui.sheet === 'settings' || document.querySelector('dialog.dialog[open]')) return
          openSettings()
          return
        case 'composer':
          if (modalOpen() || s.ui.menu) return
          if (focusComposer()) e.preventDefault()
          return
        case 'escape':
          if (s.ui.menu) return closeMenu()
          // Escape closes the panel too, unless it belongs to a text field
          // (the composer, the address bar) or the user has the browser.
          if (modalOpen() || s.ui.reflect || !s.data.session.surface.kind || s.data.session.control || isTyping(e.target instanceof Element ? e.target : null)) return
          e.preventDefault()
          closePanel()
          return
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}
