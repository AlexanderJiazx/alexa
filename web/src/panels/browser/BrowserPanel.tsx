import { useState } from 'react'
import { closePanel, handBack, takeOver, toast } from '../../state/actions'
import { liveStore, setLiveUrl, useLive } from '../../state/live'
import { useSession } from '../../state/selectors'
import { useApp } from '../../state/store'
import type { PanelProps } from '../registry'
import { runningBrowserStep } from './action'
import { BrowserView } from './BrowserView'
import { createLive } from './controller'
import { socketSource } from './source'

// BrowserPanel is the shared browser: watch Alexa work, or take over. It
// reads the session and the live store, and opens the live socket for as
// long as it's on screen.
export function BrowserPanel(_: PanelProps) {
  const s = useSession((v) => ({ status: v.status, needs: v.needs, control: v.control, browserUrl: v.browserUrl }))
  const url = useLive((l) => l.url) || s.browserUrl || ''
  const step = useApp((a) => runningBrowserStep(a.data.feed))
  const [live] = useState(() => createLive(socketSource(), { cursor: liveStore, onHello: setLiveUrl, onNotice: (t) => toast('Couldn’t open that address', t) }))
  return (
    <BrowserView
      live={live}
      cursor={liveStore}
      url={url}
      working={s.status === 'working'}
      control={s.control}
      needs={s.status === 'needs' ? (s.needs ?? null) : null}
      step={step}
      onTakeOver={takeOver}
      onHandBack={handBack}
      onClose={closePanel}
    />
  )
}
