import { Profiler, useEffect, useRef, useState, type ReactNode } from 'react'
import type { Entry, Needs } from '../../api/types'
import '../../app/layout.css'
import { Section } from '../../dev/Section'
import type { GalleryPage } from '../../dev/types'
import { BrowserView } from './BrowserView'
import { createLive, type Live } from './controller'
import type { HumanInput } from './source'
import { cursorAt, fakeSource, frames, needs, spots, steps, urls } from './fixtures'
import './gallery.css'

// The live browser's gallery page: every state the panel can be in, from
// recorded frames, with no socket and no sign-in. The last section checks
// that frames don't re-render React.

type DemoProps = {
  source: Parameters<typeof fakeSource>[0]
  cursor?: ReturnType<typeof cursorAt>
  url: string
  working?: boolean
  control?: boolean
  needs?: Needs | null
  step?: Entry | null
  stale?: boolean
  openNote?: boolean
  onLive?: (l: Live) => void
  // log shows what the view sends the page, to check take-over input by
  // hand: drag, double click, the wheel, keys.
  log?: boolean
}

const noop = () => {}

function Demo({ source, cursor, url, working = true, control = false, needs = null, step = null, stale, openNote, onLive, log }: DemoProps) {
  const [c] = useState(() => cursor ?? cursorAt(null))
  const [src] = useState(() => fakeSource(source))
  const [live] = useState(() => createLive(src, { cursor: c }))
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    onLive?.(live)
    const timers: ReturnType<typeof setTimeout>[] = []
    // Nothing has arrived for a while since Alexa acted (once the first
    // frame is in, which would otherwise count as the answer).
    let once = !stale
    const unwatch = live.view.subscribe(() => {
      if (once || !live.view.get().hasFrame) return
      once = true
      timers.push(setTimeout(() => live.expect(performance.now() - 6000), 300))
    })
    if (openNote) timers.push(setTimeout(() => box.current?.querySelector<HTMLButtonElement>('.bp-handback button')?.click(), 300))
    return () => {
      unwatch()
      timers.forEach(clearTimeout)
    }
  }, [live, stale, openNote, onLive])
  return (
    <div ref={box} className="bp-demo">
      <BrowserView live={live} cursor={c} url={url} working={working} control={control} needs={needs} step={step} onTakeOver={noop} onHandBack={noop} onClose={noop} />
      {log && <InputLog sent={src.sent} />}
    </div>
  )
}

// InputLog is the last few input events sent, newest first, with the
// buttons held (FE-04: a drag carries its button on every move).
function InputLog({ sent }: { sent: HumanInput[] }) {
  const [n, setN] = useState(0)
  useEffect(() => {
    const t = setInterval(() => setN(sent.length), 250)
    return () => clearInterval(t)
  }, [sent])
  const shown = sent.slice(Math.max(0, n - 6), n).reverse()
  return (
    <ol className="bp-log num" data-testid="input-log" data-count={n}>
      {shown.map((e, i) => (
        <li key={n - i}>
          {e.kind}
          {e.x !== undefined && ` ${e.x},${e.y}`}
          {e.button && ` ${e.button}`}
          {e.buttons !== undefined && ` buttons ${e.buttons}`}
          {e.clicks && e.clicks > 1 ? ` ×${e.clicks}` : ''}
          {e.dy ? ` dy ${Math.round(e.dy)}` : ''}
          {e.key && ` ${e.key}`}
          {e.text && e.kind === 'text' && ` “${e.text}”`}
        </li>
      ))}
    </ol>
  )
}

// RenderCheck plays frames at 12 a second and counts React commits of the
// panel next to the frames painted: the commits stay put while the frames
// climb (B09, acceptance 4). The counter is a sibling with its own state,
// so showing the numbers doesn't re-render the panel.
function RenderCheck() {
  const [probe] = useState(() => ({ commits: 0, live: null as Live | null }))
  const [panel] = useState(() => (
    <Profiler id="browser" onRender={() => probe.commits++}>
      <Demo source={{ frames: [frames.shop, frames.results, frames.code], fps: 12 }} url={urls.shop} onLive={(l) => (probe.live = l)} />
    </Profiler>
  ))
  return (
    <>
      <Counter probe={probe} />
      {panel}
    </>
  )
}

function Counter({ probe }: { probe: { commits: number; live: Live | null } }) {
  const [shown, setShown] = useState({ commits: 0, painted: 0, received: 0 })
  useEffect(() => {
    const t = setInterval(() => setShown({ commits: probe.commits, ...(probe.live?.stats() ?? { painted: 0, received: 0 }) }), 500)
    return () => clearInterval(t)
  }, [probe])
  return (
    <p className="bp-count num" data-testid="render-check">
      Frames received {shown.received} · painted {shown.painted} · React commits {shown.commits}
    </p>
  )
}

function Row({ children }: { children: ReactNode }) {
  return <div className="bp-row">{children}</div>
}

const page: GalleryPage = {
  name: 'browser',
  title: 'Browser',
  render: () => (
    <>
      <Section title="Watching: Alexa clicks “Add to cart”">
        <Demo source={{ frames: [frames.shop] }} cursor={cursorAt(spots.addToCart)} url={urls.shop} step={steps.click} />
      </Section>
      <Section title="Typing into a field">
        <Demo source={{ frames: [frames.shop] }} cursor={cursorAt(spots.search)} url={urls.shop} step={steps.type} />
      </Section>
      <Section title="Signing in: a secret never shows">
        <Demo source={{ frames: [frames.code] }} cursor={cursorAt(spots.firstDigit)} url={urls.code} step={steps.secret} />
      </Section>
      <Section title="A page loading">
        <Demo source={{ frames: [frames.results], hello: { loading: true } }} cursor={cursorAt({ x: spots.card.x, y: spots.card.y })} url={urls.results} step={steps.open} />
      </Section>
      <Section title="Alexa needs you to take over">
        <Demo source={{ frames: [frames.code] }} url={urls.code} working={false} needs={needs.takeover} />
      </Section>
      <Section title="In control, handing back with a note">
        <Demo source={{ frames: [frames.code] }} url={urls.code} working={false} control needs={needs.takeover} openNote />
      </Section>
      <Section title="In control, on your own (what reaches the page is listed below it)">
        <Demo source={{ frames: [frames.results] }} url={urls.results} working={false} control log />
      </Section>
      <Row>
        <Section title="Reconnecting">
          <Demo source={{ frames: [frames.shop], socket: 'drop' }} cursor={cursorAt(spots.addToCart)} url={urls.shop} />
        </Section>
        <Section title="Stale">
          <Demo source={{ frames: [frames.shop] }} cursor={cursorAt(spots.addToCart)} url={urls.shop} step={steps.click} stale />
        </Section>
      </Row>
      <Row>
        <Section title="Asleep">
          <Demo source={{ frames: [frames.results], socket: 'asleep' }} url={urls.results} working={false} />
        </Section>
        <Section title="Connecting: no picture yet">
          <Demo source={{ frames: [], socket: 'never' }} url="" />
        </Section>
      </Row>
      <Section title="Render check: frames don’t re-render React">
        <RenderCheck />
      </Section>
    </>
  ),
}
export default page
