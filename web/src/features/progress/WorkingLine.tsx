import { useEffect, useState } from 'react'
import { show, stop } from '../../state/actions'
import { useRun, useSession, useShowBrowser } from '../../state/selectors'
import { useApp } from '../../state/store'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { Favicon, hostName } from '../../ui/Favicon'
import { IconButton } from '../../ui/IconButton'
import { Eye, Stop } from '../../ui/icons'
import { announce } from '../../ui/LiveAnnouncer'
import { Mark } from '../../ui/Mark'
import { fmtDuration, Timer, useNow } from '../../ui/Timer'
import { StopSquare } from './glyphs'
import { createAnnouncer, useHold, useShownAfter } from './hold'
import { workingLine, type Line } from './phase'
import './progress.css'
import { currentExchange, selectLivePlan, usedBrowser } from './runState'
import { WritingSkeleton } from './WritingSkeleton'

// How long a label stays before the next replaces it, and how long the line
// waits before it shows at all, so a quick reply never flashes it.
const hold = 3000
const appear = 1500

// Screen readers hear the line's changes politely, one every ten seconds at
// most: enough to follow along, never a running commentary.
const phases = createAnnouncer((t) => announce(t), 10e3)

// WorkingLine is the line under the exchange while Alexa works: what it's on
// (the reasoning headline or the plan step), the tool it's using and where,
// how long it has been at it, and Stop.
export function WorkingLine() {
  const session = useSession((s) => ({ control: s.control, activity: s.activity, phase: s.phase, browserUrl: s.browserUrl, runStartedAt: s.runStartedAt }))
  const run = useRun()
  const showBrowser = useShowBrowser()
  const planStep = useApp((s) => selectLivePlan(s)?.plan.steps.find((p) => p.status === 'active')?.text)
  // A server without run events has no start time; the request's is close.
  const exchangeAt = useApp((s) => currentExchange(s.data.feed)[0]?.createdAt ?? 0)
  // The eye is for the page Alexa is at work on, not one left open from
  // before: it shows once this exchange has used the browser.
  const browsing = useApp((s) => usedBrowser(currentExchange(s.data.feed), s.data.run))
  const line = workingLine(session, run, planStep)
  return (
    <WorkingLineView
      line={line}
      since={session.runStartedAt ?? exchangeAt}
      control={session.control}
      onShowBrowser={!showBrowser && session.browserUrl && browsing ? () => show({ kind: 'browser' }) : undefined}
      onStop={stop}
    />
  )
}

type ViewProps = {
  line: Line
  since: number
  control?: boolean
  onShowBrowser?: () => void
  onStop?: () => void
  // appearAfter is the wait before the line shows (the gallery passes 0).
  appearAfter?: number
}

// WorkingLineView is WorkingLine without the store, for the gallery.
export function WorkingLineView({ line, since, control, onShowBrowser, onStop, appearAfter = appear }: ViewProps) {
  const [mounted] = useState(() => Date.now())
  const shown = useShownAfter(appearAfter, since || mounted)
  // While it's hidden there's nothing to flicker, so labels change at once.
  const headline = useHold(line.headline, hold, line.now || !shown)
  const subKey = line.sub ? `${line.sub.host ?? ''}\n${line.sub.label ?? ''}` : ''
  const sub = useHold(subKey, hold, line.now || !shown)
  const [subHost, subLabel] = sub ? sub.split('\n') : ['', '']

  useEffect(() => {
    if (shown) phases.push(headline)
  }, [shown, headline])
  useEffect(() => () => phases.cancel(), [])

  const stopping = line.headline === 'Stopping…'
  return (
    <div className={cx('working', shown && 'shown', control && 'control')}>
      <div className="pg-wl">
        <span className="pg-ic">
          <Mark state={control ? 'waiting' : 'working'} size="sm" />
        </span>
        <span key={headline} className={cx('pg-headline', !line.quiet && 'shimmer')}>
          {headline}
        </span>
        <span className="pg-acts">
          {/* Stop stays, disabled, while it takes effect, so nothing shifts.
              A phone gets the glyph alone (CSS picks one), so the headline
              keeps the room. */}
          {onStop && (
            <>
              <Button size="sm" variant="ghost" className="pg-stop" icon={<Stop size={14} />} onClick={onStop} disabled={stopping}>
                Stop
              </Button>
              <IconButton label="Stop" size="sm" tone="muted" className="pg-stop-icon" onClick={onStop} disabled={stopping}>
                <StopSquare />
              </IconButton>
            </>
          )}
          {onShowBrowser && (
            <IconButton label="Show the browser" size="sm" tone="muted" onClick={onShowBrowser}>
              <Eye size={17} />
            </IconButton>
          )}
        </span>
        {/* The time ends on the same edge as the steps' times above. */}
        {since > 0 && <Timer from={since} className="pg-elapsed" />}
      </div>
      {(subHost || subLabel || line.retry) && (
        <div key={sub} className="pg-sub">
          {subHost && <Favicon host={subHost} size={14} />}
          {subHost && <span className="pg-site">{hostName(subHost)}</span>}
          {subHost && subLabel && <span aria-hidden="true">·</span>}
          {subLabel && <span className="pg-sub-label">{subLabel}</span>}
          {line.retry && <Countdown at={line.retry.at} />}
        </div>
      )}
      {line.writing && <WritingSkeleton target={line.writing} />}
    </div>
  )
}

// Countdown is the time to the next try, while a model call is retried.
function Countdown({ at }: { at: number }) {
  const now = useNow()
  const left = at - now
  return (
    <>
      <span aria-hidden="true">·</span>
      <span className="pg-count num">{left > 999 ? `next try in ${fmtDuration(left)}` : 'trying now'}</span>
    </>
  )
}
