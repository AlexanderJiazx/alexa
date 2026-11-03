import type { ChangedWhat } from '../api/realtime'
import type { AppNotification, Entry, Needs, Report, SessionState } from '../api/types'
import type { RunInfo } from './reducer'
import { shallow, useApp, type AppState, type ConnectionStatus, type UI } from './store'

// Selector hooks: each reads one slice of the store and re-renders its
// component only when that slice changes. Plain selector functions are
// exported too, for tests and for code outside React.

export const selectSession = (s: AppState) => s.data.session
export const selectFeed = (s: AppState) => s.data.feed
export const selectNeeds = (s: AppState): Needs | null => (s.data.session.status === 'needs' ? (s.data.session.needs ?? null) : null)
export const selectUnread = (s: AppState) => s.data.notes.filter((n) => !n.read).length
// The browser panel shows while the surface is the browser, and always
// while the user is in control of it.
export const selectShowBrowser = (s: AppState) => s.data.session.surface.kind === 'browser' || s.data.session.control

// useSession picks from the session (the server's View). Pick fields, not
// the whole object, where the component doesn't need all of it; a pick of
// several fields ({control: s.control, ...}) compares shallowly.
export function useSession(): SessionState
export function useSession<T>(select: (s: SessionState) => T): T
export function useSession<T>(select?: (s: SessionState) => T) {
  return useApp((s) => (select ? select(s.data.session) : s.data.session), shallow)
}

export const useStatus = () => useApp((s) => s.data.session.status)
export const useWorking = () => useApp((s) => s.data.session.status === 'working')
export const useControl = () => useApp((s) => s.data.session.control)
export const useNeeds = () => useApp(selectNeeds)
export const useSurface = () => useApp((s) => s.data.session.surface)
export const useShowBrowser = () => useApp(selectShowBrowser)

export const useFeed = (): Entry[] => useApp(selectFeed)
export const useHasMore = () => useApp((s) => s.data.hasMore)
export const useLoaded = () => useApp((s) => s.data.loaded)
// useEntry finds one entry of the loaded feed.
export const useEntry = (id: number): Entry | undefined => useApp((s) => s.data.feed.find((e) => e.id === id))

export const useReport = (id: string | undefined): Report | null => useApp((s) => (id ? (s.data.reports[id] ?? null) : null))
export const useNotes = (): AppNotification[] => useApp((s) => s.data.notes)
export const useUnread = () => useApp(selectUnread)
export const useRun = (): RunInfo | null => useApp((s) => s.data.run)
// useChanged counts server-side changes of one kind (WP-18): put it in an
// effect's deps to refetch when the data moves.
export const useChanged = (what: ChangedWhat) => useApp((s) => s.data.changed[what] ?? 0)

export const useBoot = () => useApp((s) => s.boot)
export const useSetup = () => useApp((s) => s.setup)
export const useUser = () => useApp((s) => s.user)
export const useConfig = () => useApp((s) => s.config)
export const useConnection = (): ConnectionStatus => useApp((s) => s.connection)

// useUI picks from the screen's UI state; several fields at once compare
// shallowly.
export function useUI<T>(select: (u: UI) => T): T {
  return useApp((s) => select(s.ui), shallow)
}
