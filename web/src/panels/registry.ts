import type { ComponentType } from 'react'
import { BrowserPanel } from './browser/BrowserPanel'
import { EntryPanel } from './entry/EntryPanel'
import { ReportPanel } from './report/ReportPanel'

// id is the rest of the surface key: the report's id for report:<id>, the
// entry's id for entry:<id>.
export type PanelProps = { id: string }

// The panels the screen can show next to the exchange, by surface kind.
// Adding one is a component plus a line here.
export const panels: Record<string, ComponentType<PanelProps>> = {
  browser: BrowserPanel,
  report: ReportPanel,
  entry: EntryPanel,
}
