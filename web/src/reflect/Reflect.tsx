import { useEffect, useId, useState } from 'react'
import { closeReflect, loadMore, setReflectTab } from '../state/actions'
import { useFeed, useHasMore, useUI } from '../state/selectors'
import type { ReflectTab } from '../state/store'
import { cx } from '../ui/cx'
import { Back, Search } from '../ui/icons'
import { IconButton } from '../ui/IconButton'
import { Tabs } from '../ui/Segmented'
import { useModal } from '../ui/useModal'
import { Files } from './FilesTab'
import { History } from './History'
import './reflect.css'
import { Reports } from './ReportsTab'

const tabs: readonly (readonly [ReflectTab, string])[] = [
  ['conversation', 'Conversation'],
  ['reports', 'Reports'],
  ['files', 'Files'],
]

const placeholders: Record<ReflectTab, string> = {
  conversation: 'Search the conversation',
  reports: 'Search reports',
  files: 'Search files',
}

// Reflect is where the past lives: the whole conversation with Alexa, the
// reports it wrote and the files in the workspace. The main screen only
// shows what's happening now. It's a page pushed over the main screen, as a
// modal <dialog>: the screen behind is inert, Escape or Back closes it and
// focus goes back where it was (FE-07).
export function Reflect({ open }: { open: boolean }) {
  const m = useModal(open, closeReflect, 200)
  if (!m.shown) return null
  return (
    <dialog ref={m.ref} className={cx('reflect', m.leaving && 'leaving')} aria-label="Reflect" onKeyDown={m.onKeyDown}>
      <ReflectPage />
    </dialog>
  )
}

// ReflectPage is what's inside: the head, the search and the tab's pane.
// The gallery pins a tab with show.
export function ReflectPage({ show }: { show?: ReflectTab }) {
  const current = useUI((u) => u.reflectTab)
  const tab = show ?? current
  const feed = useFeed()
  const hasMore = useHasMore()
  const [query, setQuery] = useState('')
  const panel = useId()
  // Each tab searches its own things.
  useEffect(() => setQuery(''), [tab])
  return (
    <>
      <header className="reflect-head">
        <IconButton label="Back to Alexa" title="Back (Esc)" onClick={closeReflect}>
          <Back size={20} />
        </IconButton>
        <h1>Reflect</h1>
        <Tabs label="What to look back on" value={tab} options={tabs} onChange={setReflectTab} className="reflect-tabs" />
      </header>
      <div className="reflect-tools">
        <label className="reflect-search">
          <Search size={16} />
          <span className="sr">{placeholders[tab]}</span>
          <input type="search" className="input" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={placeholders[tab]} autoComplete="off" spellCheck={false} />
        </label>
      </div>
      <div className="reflect-body" role="tabpanel" id={panel} aria-label={tabs.find(([t]) => t === tab)?.[1]}>
        {tab === 'conversation' && <History feed={feed} hasMore={hasMore} onMore={loadMore} query={query} />}
        {tab === 'reports' && <Reports query={query} />}
        {tab === 'files' && <Files query={query} />}
      </div>
    </>
  )
}
