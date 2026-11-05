import { createElement, type ComponentType } from 'react'
import type { Entry } from '../../api/types'
import type { Answer } from '../ask/answer'
import { AskEntry } from './entries/AskEntry'
import { ErrorEntry } from './entries/ErrorEntry'
import { FallbackEntry } from './entries/FallbackEntry'
import { LoginEntry } from './entries/LoginEntry'
import { NoteEntry } from './entries/NoteEntry'
import { PlanEntry } from './entries/PlanEntry'
import { ReportEntry } from './entries/ReportEntry'
import { SayEntry } from './entries/SayEntry'
import { TakeoverEntry } from './entries/TakeoverEntry'
import { UserEntry } from './entries/UserEntry'

// now: the current exchange, on the main screen; history: the whole
// conversation, in Reflect, where nothing can be answered any more.
export type EntryMode = 'now' | 'history'

// compact is the smaller form for tight places (the caption under a panel
// on small screens). answer is, for an ask no longer waiting, how it was
// answered, when whoever draws it knows the entries around it (the log).
export type EntryProps = { entry: Entry; mode: EntryMode; compact?: boolean; answer?: Answer | null }

export type EntryDef = {
  Component: ComponentType<EntryProps>
  // announce is what a screen reader hears when the entry arrives.
  announce?: (e: Entry) => string | null
}

// The entry kinds the conversation shows. Adding a kind is one file in
// entries/ plus one line here. Steps aren't here: the log groups runs of
// them into a StepsBlock.
export const entryKinds: Record<string, EntryDef> = {
  user: { Component: UserEntry },
  say: { Component: SayEntry, announce: (e) => e.text },
  ask: { Component: AskEntry, announce: (e) => e.text },
  takeover: { Component: TakeoverEntry, announce: (e) => e.text },
  login: { Component: LoginEntry, announce: (e) => e.text },
  report: { Component: ReportEntry, announce: (e) => `Report ready: ${e.text}` },
  error: { Component: ErrorEntry, announce: (e) => e.text },
  note: { Component: NoteEntry },
  plan: { Component: PlanEntry },
}

// Kinds this client doesn't know (from a newer server) show their text.
const fallback: EntryDef = { Component: FallbackEntry }

export function entryDef(kind: string): EntryDef {
  return entryKinds[kind] ?? fallback
}

export function renderEntry(entry: Entry, mode: EntryMode, compact?: boolean, answer?: Answer | null) {
  return createElement(entryDef(entry.kind).Component, { entry, mode, compact, answer })
}
