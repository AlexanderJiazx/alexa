import { useEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import type { ServerConfig, User } from '../api/types'
import { initialData, reduce, type Action, type Data } from './reducer'

// A small external store: components read slices of it through selector
// hooks (useSyncExternalStore), so an event re-renders only what shows the
// part that changed (FE-06). There is no library and no context.

export type Store<S> = {
  get: () => S
  set: (next: S | ((s: S) => S)) => void
  subscribe: (fn: () => void) => () => void
}

export function createStore<S>(initial: S): Store<S> {
  let state = initial
  const subs = new Set<() => void>()
  return {
    get: () => state,
    set(next) {
      const n = typeof next === 'function' ? (next as (s: S) => S)(state) : next
      if (Object.is(n, state)) return
      state = n
      subs.forEach((f) => f())
    },
    subscribe(fn) {
      subs.add(fn)
      return () => {
        subs.delete(fn)
      }
    },
  }
}

// useSelector returns select(state) and re-renders only when that changes
// (by eq, Object.is unless given). A selector may build a new object each
// time as long as eq says when two are the same.
export function useSelector<S, T>(store: Store<S>, select: (s: S) => T, eq: (a: T, b: T) => boolean = Object.is): T {
  const last = useRef<{ has: boolean; value: T }>({ has: false, value: undefined as T })
  const get = useMemo(() => {
    let memo: { s: S; t: T } | null = null
    return () => {
      const s = store.get()
      if (memo && Object.is(memo.s, s)) return memo.t
      const t = select(s)
      const prev = memo ? memo.t : last.current.has ? last.current.value : undefined
      if ((memo || last.current.has) && eq(prev as T, t)) {
        memo = { s, t: prev as T }
        return prev as T
      }
      memo = { s, t }
      return t
    }
  }, [store, select, eq])
  const value = useSyncExternalStore(store.subscribe, get, get)
  useEffect(() => {
    last.current = { has: true, value }
  }, [value])
  return value
}

// shallow compares two plain objects or arrays one level deep, for
// selectors that pick several fields at once.
export function shallow<T>(a: T, b: T): boolean {
  if (Object.is(a, b)) return true
  if (typeof a !== 'object' || typeof b !== 'object' || !a || !b) return false
  const ka = Object.keys(a as object)
  const kb = Object.keys(b as object)
  if (ka.length !== kb.length) return false
  for (const k of ka) if (!Object.is((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k])) return false
  return true
}

// ---------- the app's store ----------

export type Boot = 'loading' | 'auth' | 'ready'
export type ConnectionStatus = 'connecting' | 'live' | 'reconnecting' | 'offline'
export type SheetName = 'schedules' | 'memory' | 'logins' | 'notifications' | 'settings' | 'users'
// Settings' sections, in the order its side lists them. Memory, Logins,
// Scheduled and Users open there (openSheet); the Notifications inbox is a
// sheet of its own.
export type SettingsSection = 'general' | 'notifications' | 'memory' | 'logins' | 'schedules' | 'conversation' | 'account' | 'users'
export type ReflectTab = 'conversation' | 'reports' | 'files'
// UI is the state of the screen around the conversation: which overlay is
// open, and voice mode. Toasts have their own queue (sheets/toasts.ts).
export type UI = {
  menu: boolean
  sheet: SheetName | null
  // section is Settings' section; null is its first screen (General on a
  // wide screen, the list of sections on a phone).
  section: SettingsSection | null
  reflect: boolean
  reflectTab: ReflectTab
  voice: boolean
}

export type AppState = {
  boot: Boot
  setup: { needsSetup: boolean; signupAllowed: boolean }
  user: User | null
  config: ServerConfig | null
  connection: ConnectionStatus
  // data is the client's copy of the session, kept by the reducer.
  data: Data
  ui: UI
}

export const initialUI: UI = { menu: false, sheet: null, section: null, reflect: false, reflectTab: 'conversation', voice: false }

export const initialState: AppState = {
  boot: 'loading',
  setup: { needsSetup: false, signupAllowed: false },
  user: null,
  config: null,
  connection: 'connecting',
  data: initialData,
  ui: initialUI,
}

export const store = createStore<AppState>(initialState)

export function dispatch(a: Action) {
  store.set((s) => {
    const data = reduce(s.data, a)
    return data === s.data ? s : { ...s, data }
  })
}

export function setUI(patch: Partial<UI> | ((u: UI) => Partial<UI>)) {
  store.set((s) => {
    const p = typeof patch === 'function' ? patch(s.ui) : patch
    const keys = Object.keys(p) as (keyof UI)[]
    if (keys.every((k) => Object.is(s.ui[k], p[k]))) return s
    return { ...s, ui: { ...s.ui, ...p } }
  })
}

export function setApp(patch: Partial<Omit<AppState, 'data' | 'ui'>>) {
  store.set((s) => {
    const keys = Object.keys(patch) as (keyof typeof patch)[]
    if (keys.every((k) => Object.is(s[k], patch[k]))) return s
    return { ...s, ...patch }
  })
}

// useApp reads the app store.
export function useApp<T>(select: (s: AppState) => T, eq?: (a: T, b: T) => boolean): T {
  return useSelector(store, select, eq)
}
