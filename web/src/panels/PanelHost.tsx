import { surfaceKey, useFollow } from '../state/exchange'
import type { AppState } from '../state/store'
import { useExit } from '../lib/useExit'
import { panels } from './registry'

// panelKey says which panel the session's surface calls for: browser,
// report:<id> or entry:<id>. The browser shows whenever the user is in
// control of it. What the screen shows can differ for a moment: see
// usePanel.
export const panelKey = (s: AppState) => surfaceKey(s.data.session)

// usePanel is the panel to show and whether it's on its way out: it stays a
// moment after it closes, to animate out. It follows the session's surface
// through the follow guards (state/exchange.ts), so Alexa never pulls the
// panel out from under the user.
export function usePanel() {
  const key = useFollow((f) => f.shown)
  return useExit(key, 380)
}

// PanelHost shows the one panel for a surface key. Each panel mounts once:
// it isn't kept around hidden for another layout (FE-17).
export function PanelHost({ panel }: { panel: string }) {
  const i = panel.indexOf(':')
  const kind = i < 0 ? panel : panel.slice(0, i)
  const id = i < 0 ? '' : panel.slice(i + 1)
  const P = panels[kind]
  return P ? <P key={panel} id={id} /> : null
}
