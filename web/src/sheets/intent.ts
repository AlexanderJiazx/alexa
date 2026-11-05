import { useEffect, useState } from 'react'
import { openSheet } from '../state/actions'
import { createStore, useSelector, type SheetName } from '../state/store'

// A section of Settings can be opened straight into what someone came to
// do: the palette's "New schedule" opens Scheduled with its form out, and
// the home checklist's "Tell Alexa where you live" opens General at About
// you. The section reads the intent as it mounts and it's forgotten once
// it's on screen (in an effect, so a render that runs twice reads the same).

export type SheetIntent = 'new' | 'about'

let wanted: { sheet: SheetName; intent: SheetIntent } | null = null

// Each intent counts, so Settings can draw its section afresh to take one
// (Scheduled already open when the palette's New schedule runs).
const asked = createStore(0)

export function openSheetFor(sheet: SheetName, intent: SheetIntent) {
  wantIntent(sheet, intent)
  openSheet(sheet)
}

// wantIntent sets the intent for the next time the section mounts.
export function wantIntent(sheet: SheetName, intent: SheetIntent) {
  wanted = { sheet, intent }
  asked.set((n) => n + 1)
}

// useIntent returns the intent this section was opened with, if any.
export function useIntent(sheet: SheetName): SheetIntent | null {
  const [intent] = useState(() => (wanted?.sheet === sheet ? wanted.intent : null))
  useEffect(() => {
    if (wanted?.sheet === sheet) wanted = null
  }, [sheet])
  return intent
}

// useIntentCount changes with each intent wanted.
export const useIntentCount = () => useSelector(asked, (n) => n)
