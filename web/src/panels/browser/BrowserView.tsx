import { useEffect, useRef, type CSSProperties } from 'react'
import type { Entry, Needs } from '../../api/types'
import type { Live as LiveStore } from '../../state/live'
import { useSelector, type Store } from '../../state/store'
import { cx } from '../../ui/cx'
import { actionFor } from './action'
import './browser.css'
import { Chrome } from './Chrome'
import type { Live } from './controller'
import { LiveScreen } from './LiveScreen'
import { Strip } from './Strip'

export type BrowserViewProps = {
  live: Live
  // cursor is the store Alexa's pointer moves arrive in.
  cursor: Store<LiveStore>
  url: string
  working: boolean
  control: boolean
  needs: Needs | null
  // step is the browser step in flight, if any.
  step: Entry | null
  onTakeOver: () => void
  onHandBack: (note: string) => void
  onClose: () => void
}

// Steps that should change what the page looks like: if no frame follows
// one, the picture is stale. Reading the page changes nothing.
const changes = new Set(['browser_click', 'browser_click_at', 'browser_type', 'browser_key', 'browser_scroll', 'browser_select', 'browser_drag', 'browser_history', 'browser_login'])
const loads = new Set(['browser_open', 'browser_history'])

// BrowserView is the shared browser panel, given everything it shows: one
// frame with the page edge to edge under a single head (review-visual 5.6).
// BrowserPanel feeds it from the store; the gallery from fixtures.
export function BrowserView({ live, cursor, url, working, control, needs, step, onTakeOver, onHandBack, onClose }: BrowserViewProps) {
  useEffect(() => live.start(), [live])

  // A new address means a page is loading until it paints. The address
  // the view opened on isn't new (and running this effect twice, as
  // development does, doesn't make it so).
  const lastUrl = useRef(url)
  useEffect(() => {
    if (url && url !== lastUrl.current) live.navigated()
    lastUrl.current = url
  }, [url, live])

  const tool = step?.data?.step?.tool ?? ''
  useEffect(() => {
    if (changes.has(tool)) live.expect()
    live.setStepLoading(loads.has(tool))
  }, [step?.id, tool, live])

  // When Alexa stops and nobody's driving, nothing more is coming.
  const quiet = !working && !control
  useEffect(() => {
    if (quiet) live.settle()
  }, [quiet, live])

  // The page's shape sizes the panel (browser.css). It changes when the
  // server says hello, not with frames.
  const size = useSelector(live.view, (v) => v.size)
  const shape = { '--ar': `${size.w} / ${size.h}`, '--arn': size.w / size.h } as CSSProperties

  // Handing back carries on Alexa's work when there is any: a run paused
  // for the user, or something it asked for.
  const asked = needs?.kind === 'takeover'
  return (
    <section className={cx('panel', 'bp', control && 'control')} style={shape} aria-label="Shared browser">
      <Chrome live={live} url={url} control={control} waiting={working || !!needs} asked={asked} onTakeOver={onTakeOver} onHandBack={onHandBack} onClose={onClose} />
      <div className="panel-body bp-body">
        <Strip control={control} needs={needs} working={working} />
        <div className="bp-stage">
          <LiveScreen live={live} control={control} cursor={cursor} action={control ? null : actionFor(step)} working={working} />
        </div>
      </div>
    </section>
  )
}
