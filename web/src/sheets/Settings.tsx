import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import type { User } from '../api/types'
import { useMedia } from '../lib/useMedia'
import { compact, resetConversation } from '../state/actions'
import { useConfig, useSession, useUI, useUser } from '../state/selectors'
import { setUI, type SettingsSection } from '../state/store'
import { cx } from '../ui/cx'
import { Field, Input } from '../ui/Field'
import { focusables, moveIndex } from '../ui/focus'
import { IconButton } from '../ui/IconButton'
import { Back, Bell, Brain, Chat, Clock, Close, Fwd, Gear, KeyIcon, People, Person, Search } from '../ui/icons'
import { announce } from '../ui/LiveAnnouncer'
import { useModal } from '../ui/useModal'
import { ConversationSection } from './ConversationSection'
import { useIntentCount } from './intent'
import { LoginsPane } from './LoginsPane'
import { MemoryPane } from './MemoryPane'
import { HeadSlot, SettingScope } from './parts'
import type { SheetProps } from './registry'
import { SchedulesPane } from './SchedulesPane'
import { findSettings, sectionsFor, sectionTitle, type Setting } from './settingsIndex'
import { AccountPane, GeneralPane, PushPane } from './SettingsSheet'
import './sheets.css'
import { UsersPane } from './UsersPane'

// Below this width Settings is the whole screen, one step at a time: the
// sections first, then the one chosen, with a way back.
const phoneQuery = '(max-width: 767px)'

// How long a setting the search found keeps its wash (sheets.css fades it).
const foundMs = 1500

const reduceMotion = typeof matchMedia === 'function' ? matchMedia('(prefers-reduced-motion: reduce)') : null

const icons: Record<SettingsSection, ReactNode> = {
  general: <Gear size={18} />,
  notifications: <Bell size={18} />,
  memory: <Brain size={18} />,
  logins: <KeyIcon size={18} />,
  schedules: <Clock size={18} />,
  conversation: <Chat size={18} />,
  account: <Person size={18} />,
  users: <People size={18} />,
}

// Settings is one large window for everything Alexa lets you set: the
// sections and a search down the side, the chosen section beside them. It's
// a native modal <dialog> (useModal): focus stays in it, Escape closes it
// and focus goes back to what opened it. openSheet and openSettings say
// which section it shows (ui.section).
export function Settings({ open, onClose }: SheetProps) {
  const section = useUI((u) => u.section)
  const m = useModal(open, onClose, 200)
  const id = useId()
  if (!m.shown) return null
  return (
    <dialog ref={m.ref} className={cx('settings', m.leaving && 'leaving')} aria-labelledby={id} onKeyDown={m.onKeyDown} onClick={m.onClick}>
      <SettingsFrame titleId={id} section={section} onSection={(s) => setUI({ section: s })} onClose={onClose} />
    </dialog>
  )
}

// SettingsInline is the same window drawn in place, for the gallery, with
// the section kept here rather than in the store, and a search typed in.
export function SettingsInline({ initial = null, query }: { initial?: SettingsSection | null; query?: string }) {
  const [section, setSection] = useState(initial)
  const id = useId()
  return (
    <section className="settings settings-inline" aria-labelledby={id}>
      <SettingsFrame titleId={id} section={section} onSection={setSection} onClose={() => setSection(null)} query={query} />
    </section>
  )
}

type FrameProps = {
  titleId: string
  section: SettingsSection | null
  onSection: (s: SettingsSection | null) => void
  onClose: () => void
  query?: string
}

// What to do once a section is drawn: show a setting the search found,
// put the focus on the section's title (a phone, where the list has gone),
// or on a section in the list (a phone, coming back).
type After = { show: string } | { title: true } | { item: SettingsSection }

function SettingsFrame({ titleId, section, onSection, onClose, query: typed = '' }: FrameProps) {
  const user = useUser() as User
  const config = useConfig()
  const phone = useMedia(phoneQuery)
  const ctx = { isAdmin: !!user.isAdmin, email: !!config?.emailEnabled || !!user.email }
  const list = sectionsFor(ctx)
  // A section this person can't see (Users, for someone who isn't an
  // admin) is General.
  const asked = section && !list.some((s) => s.id === section) ? 'general' : section
  // With none chosen, a wide screen shows General; a phone, the list.
  const shown = asked ?? (phone ? null : 'general')
  const [query, setQuery] = useState(typed)
  const searching = query.trim() !== ''
  const found = findSettings(query, ctx)
  const scope = `${useId()}-`
  const intents = useIntentCount()
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  const side = useRef<HTMLDivElement>(null)
  const title = useRef<HTMLHeadingElement>(null)
  const after = useRef<After | null>(null)

  // After every draw, do what choosing asked for (the section it chose may
  // be the one already shown, so this doesn't wait for the section to change).
  useEffect(() => {
    const a = after.current
    if (!a) return
    after.current = null
    if ('item' in a) side.current?.querySelector<HTMLElement>(`[data-section="${a.item}"]`)?.focus()
    else if ('title' in a) title.current?.focus({ preventScroll: true })
    else reveal(document.getElementById(scope + a.show), title.current, phone)
  })

  // What a search finds is read out once the typing settles.
  useEffect(() => {
    if (!searching) return
    const t = setTimeout(() => announce(found.length ? `${found.length} ${found.length === 1 ? 'setting' : 'settings'} found` : 'No settings match'), 600)
    return () => clearTimeout(t)
  }, [query])

  const pick = (s: SettingsSection) => {
    if (phone) after.current = { title: true }
    onSection(s)
  }
  const choose = (r: Setting) => {
    after.current = { show: r.key }
    setQuery('')
    onSection(r.section)
  }
  const back = () => {
    if (shown) after.current = { item: shown }
    onSection(null)
  }

  // Up and Down move through the sections or what was found, Home and End
  // go to the ends; Tab goes on to the section.
  const arrows = (e: KeyboardEvent<HTMLElement>) => {
    const items = Array.from(e.currentTarget.querySelectorAll<HTMLElement>('button'))
    const i = moveIndex(e.key, items.indexOf(document.activeElement as HTMLElement), items.length, 'vertical')
    if (i === null) return
    e.preventDefault()
    items[i].focus()
  }
  const onSearchKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape' && query) {
      // The first Escape empties the search; the next closes Settings.
      e.preventDefault()
      e.stopPropagation()
      setQuery('')
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      side.current?.querySelector<HTMLElement>('.settings-list button')?.focus()
    } else if (e.key === 'Enter' && !e.nativeEvent.isComposing && found[0]) {
      e.preventDefault()
      choose(found[0])
    }
  }

  const close = (
    <IconButton label="Close" onClick={onClose}>
      <Close size={18} />
    </IconButton>
  )

  return (
    <SettingScope.Provider value={scope}>
      <div ref={side} className="settings-side" hidden={phone && !!shown}>
        <div className="settings-side-head">
          <h2 id={titleId}>Settings</h2>
          {phone && close}
        </div>
        <div className="search-row settings-search">
          <Search size={16} />
          <Field label="Search settings" hideLabel flush>
            {(id) => <Input id={id} type="search" value={query} onChange={(e) => setQuery(e.target.value)} onKeyDown={onSearchKey} placeholder="Search" autoComplete="off" spellCheck={false} enterKeyHint="go" />}
          </Field>
        </div>
        <div className="settings-nav">
          {searching ? (
            found.length ? (
              <ul className="settings-list" aria-label="Settings found" onKeyDown={arrows}>
                {found.map((r) => {
                  // A result that is a whole section doesn't name it twice.
                  const sub = sectionTitle(r.section)
                  return (
                    <li key={r.key}>
                      <button type="button" className={cx('settings-item', sub !== r.title && 'two')} onClick={() => choose(r)}>
                        {icons[r.section]}
                        <span className="settings-item-text">
                          <span className="settings-item-label">{r.title}</span>
                          {sub !== r.title && <span className="settings-item-sub">{sub}</span>}
                        </span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            ) : (
              <p className="settings-none">No settings match “{query.trim()}”.</p>
            )
          ) : (
            <nav aria-label="Sections">
              <ul className="settings-list" onKeyDown={arrows}>
                {list.map((s) => (
                  <li key={s.id}>
                    <button type="button" className="settings-item" data-section={s.id} aria-current={s.id === shown ? 'page' : undefined} onClick={() => pick(s.id)}>
                      {icons[s.id]}
                      <span className="settings-item-label">{s.title}</span>
                      {phone && <Fwd size={16} />}
                    </button>
                  </li>
                ))}
              </ul>
            </nav>
          )}
        </div>
      </div>
      {shown && (
        <section className="settings-main" aria-labelledby={`${titleId}-section`}>
          <div className="settings-head">
            {phone && (
              <IconButton label="Back to Settings" onClick={back}>
                <Back size={20} />
              </IconButton>
            )}
            <h2 id={`${titleId}-section`} ref={title} tabIndex={-1}>
              {sectionTitle(shown)}
            </h2>
            <div ref={setSlot} className="settings-actions" />
            {close}
          </div>
          <HeadSlot.Provider value={slot}>
            {/* Drawn afresh for each section, and for each intent (the
                palette's New schedule with Scheduled open), so it starts at
                the top and reads what it was opened for. */}
            <div key={`${shown}:${intents}`} className="settings-body">
              <Pane section={shown} />
            </div>
          </HeadSlot.Provider>
        </section>
      )}
    </SettingScope.Provider>
  )
}

function Pane({ section }: { section: SettingsSection }) {
  switch (section) {
    case 'general':
      return <GeneralPane />
    case 'notifications':
      return <PushPane />
    case 'memory':
      return <MemoryPane />
    case 'logins':
      return <LoginsPane />
    case 'schedules':
      return <SchedulesPane />
    case 'conversation':
      return <ConversationPane />
    case 'account':
      return <AccountPane />
    case 'users':
      return <UsersPane />
  }
}

function ConversationPane() {
  const session = useSession()
  return <ConversationSection session={session} onCompact={compact} onReset={resetConversation} />
}

// reveal shows a setting the search found: scrolled into view with a wash
// that fades, and the focus on its control (on a wide screen; a phone puts
// it on the section's title, so no keyboard comes up unasked). A whole
// section (Memory) has no row: the focus goes to its title.
function reveal(el: HTMLElement | null, title: HTMLElement | null, phone: boolean) {
  if (!el) return title?.focus({ preventScroll: true })
  const box = el.closest<HTMLElement>('.field') ?? el
  box.scrollIntoView({ block: 'center', behavior: reduceMotion?.matches ? 'auto' : 'smooth' })
  box.classList.remove('found')
  void box.offsetWidth
  box.classList.add('found')
  setTimeout(() => box.classList.remove('found'), foundMs)
  const control = phone ? null : el.matches('input, select, textarea') ? el : focusables(box)[0]
  ;(control ?? title)?.focus({ preventScroll: true })
}
