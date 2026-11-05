import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { Composer } from '../composer/Composer'
import { composerEmpty } from '../composer/focus'
import { VoiceTray } from '../composer/VoiceTray'
import { NeedsDock } from '../features/ask/NeedsDock'
import { BackgroundChip } from '../features/caption/BackgroundChip'
import { Caption } from '../features/caption/Caption'
import { FollowPill } from '../features/caption/FollowPill'
import { Log } from '../features/conversation/Log'
import { HomeBoard } from '../features/home/HomeBoard'
import { RunCard } from '../features/progress/RunCard'
import { useExit } from '../lib/useExit'
import { useMedia } from '../lib/useMedia'
import { PanelHost, usePanel } from '../panels/PanelHost'
import { PanelSheet, type Detent } from '../panels/PanelSheet'
import { primeFollowing, startFollowing, useBackgroundJob, useExchange, useExchangeKeeper } from '../state/exchange'
import { useLoaded, useSession, useUI, useWorking } from '../state/selectors'
import { cx } from '../ui/cx'
import { focusables } from '../ui/focus'
import { boardTab } from './tabOrder'
import './layout.css'

// Stage is the main screen: the current exchange (or the home board), at
// most one panel (the live browser, a report, an expanded entry) and the
// composer. From 900px the panel sits beside the exchange column and the
// composer docks under the column; under that the panel is a bottom sheet
// over the conversation. Which panel shows follows the session,
// through the follow guards (state/exchange.ts).
export function Stage() {
  useExchangeKeeper()
  const main = useRef<HTMLElement>(null)
  const primed = useRef(false)
  if (!primed.current) {
    primed.current = true
    primeFollowing()
  }
  useEffect(() => startFollowing(() => readGuards(main.current)), [])

  const exchange = useExchange()
  const job = useBackgroundJob()
  const working = useWorking()
  const loaded = useLoaded()
  const voice = useUI((u) => u.voice)
  const mustAct = useSession((s) => s.control || (s.status === 'needs' && (s.needs?.kind === 'takeover' || s.needs?.kind === 'login')))
  const { shown: panel, leaving } = usePanel()
  const narrow = useMedia('(max-width: 899px)')
  const phone = useMedia('(max-width: 640px)')

  // The work going on is this exchange's unless it's a background job.
  const live = working && !job
  const home = loaded && exchange.length === 0 && !live
  const board = useExit(home ? true : null, 220)
  // The composer sits in the board, under the greeting, while the board has
  // the screen to itself.
  const centredNow = home && !panel && !voice && !phone
  const wasCentred = useRef(centredNow)
  if (!board.leaving) wasCentred.current = centredNow
  const slot = useSlot(main, centredNow)
  // The column sits beside a panel (not while the panel goes).
  const beside = !!panel && !narrow && !leaving
  const morph = useMorph(voice)
  useGlide(main, { centred: centredNow, placed: slot.placed, beside }, loaded)
  useColumnGlide(main, beside)
  useBoardTabOrder(main, centredNow && slot.placed)

  const [detent, setDetent] = useDetent(panel, mustAct)
  const sheet = narrow && panel ? panel : null

  return (
    <>
      <main
        ref={main}
        className={cx(
          'stage',
          panel && 'with-panel',
          panel === 'browser' && 'with-browser',
          leaving && 'closing',
          home && 'home-on',
          centredNow && 'home-centred',
          centredNow && slot.placed && 'home-placed',
          sheet && !leaving && `sheet-${detent}`,
          morph && 'morph',
        )}
      >
        {panel && !narrow && (
          <div className="panel-col">
            <PanelHost panel={panel} />
          </div>
        )}
        {board.shown && <HomeBoard centred={board.leaving ? wasCentred.current : centredNow} slot={slot.ref} leaving={board.leaving} />}
        {!home && <Log mode="now" entries={exchange} live={live} head={job ? <BackgroundChip label={job} /> : <RunCard />} />}
        <div className="follow-slot">
          <FollowPill />
        </div>
      </main>
      {sheet && <PanelSheet panel={sheet} leaving={leaving} detent={detent} onDetent={setDetent} foot={detent === 'full' ? <Caption /> : null} />}
      <NeedsDock />
      {voice && <VoiceTray />}
      <Composer />
    </>
  )
}

// readGuards is what the follow guards need to know from the screen at the
// moment Alexa switches the panel (Appendix C.2).
function readGuards(main: HTMLElement | null) {
  const log = main?.querySelector<HTMLElement>('.log')
  return {
    typing: !composerEmpty(),
    scrolled: !!log && log.scrollHeight - log.scrollTop - log.clientHeight > 120,
  }
}

// useDetent is the sheet's height. A panel opens at half, so the
// conversation and the composer stay in view; one the user has to act in
// (a take-over, a sign-in) opens at full.
function useDetent(panel: string | null, mustAct: boolean) {
  const [detent, setDetent] = useState<Detent>(mustAct ? 'full' : 'half')
  const [seen, setSeen] = useState({ panel, mustAct })
  if (seen.panel !== panel || seen.mustAct !== mustAct) {
    setSeen({ panel, mustAct })
    if (mustAct && !seen.mustAct) setDetent('full')
    else if (panel && panel !== seen.panel) setDetent(mustAct && panel === 'browser' ? 'full' : 'half')
  }
  return [detent, setDetent] as const
}

// useSlot finds where the composer goes in the board and tells the
// composer, through --home-bar on the app: how far the slot's top is from
// the top of the app. The composer is the stage's sibling, so the stage's
// CSS places it from there (layout.css).
function useSlot(main: RefObject<HTMLElement | null>, on: boolean) {
  const ref = useRef<HTMLDivElement>(null)
  const [placed, setPlaced] = useState(false)
  useLayoutEffect(() => {
    const el = ref.current
    const app = main.current?.closest<HTMLElement>('.app')
    if (!app) return
    if (!on || !el) {
      app.style.removeProperty('--home-bar')
      setPlaced(false)
      return
    }
    const measure = () => app.style.setProperty('--home-bar', `${Math.round(el.getBoundingClientRect().top - app.getBoundingClientRect().top)}px`)
    measure()
    setPlaced(true)
    // The greeting's font arriving, or the window resizing, moves the slot.
    const ro = new ResizeObserver(measure)
    if (el.previousElementSibling) ro.observe(el.previousElementSibling)
    ro.observe(app)
    return () => {
      ro.disconnect()
      app.style.removeProperty('--home-bar')
    }
  }, [main, on])
  return { ref, placed }
}

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
const moveEase = 'cubic-bezier(.65, 0, .35, 1)' // --ease-in-out
const moveMs = 480 // --d-4

type Place = { centred: boolean; placed: boolean; beside: boolean }

// useGlide moves the composer between its places smoothly (a FLIP): the
// home board, the dock under the conversation and the dock under the
// column beside a panel. It jumps to where it belongs, then slides there
// from where it was, by transform only. Only a move between places glides;
// a resize doesn't, and nor does the composer finding its first place as
// the app opens.
function useGlide(main: RefObject<HTMLElement | null>, place: Place, loaded: boolean) {
  const last = useRef<{ x: number; y: number; place: Place; settled: boolean } | null>(null)
  const anim = useRef<Animation | null>(null)
  useLayoutEffect(() => {
    const bar = main.current?.parentElement?.querySelector<HTMLElement>(':scope > .bar')
    if (!bar) return
    // Its layout top edge and middle: where the CSS puts it, untouched by
    // the glide's own transform. Not the bottom edge: the bar's height
    // eases between the board's and the dock's, so a height read at the
    // last render can be stale, and the glide would start with a jump.
    const y = bar.offsetTop
    const x = bar.offsetLeft + bar.offsetWidth / 2
    const prev = last.current
    last.current = { x, y, place, settled: !!prev?.settled || (loaded && (!place.centred || place.placed)) }
    if (!prev?.settled || samePlace(prev.place, place) || reducedMotion()) return
    // Up or down between the board and the dock; sideways to and from the
    // column beside a panel. Each only when that's what changed: the bar's
    // height changes as text is typed without a render here.
    const dy = prev.place.centred !== place.centred || prev.place.placed !== place.placed ? prev.y - y : 0
    const dx = prev.place.beside !== place.beside ? prev.x - x : 0
    if (Math.abs(dx) < 2 && Math.abs(dy) < 2) return
    anim.current?.cancel()
    // A move sideways changes the bar's width too: it fades up from a
    // little lighter so the jump in width doesn't read as one.
    const sideways = Math.abs(dx) >= 2
    anim.current = bar.animate(
      [
        { transform: `translate(${dx}px, ${dy}px)`, opacity: sideways ? 0.6 : 1 },
        { transform: 'translate(0, 0)', opacity: 1 },
      ],
      { duration: sideways ? moveMs : 320, easing: moveEase },
    )
  })
  // A resize moves the bar without a render: the next glide starts from
  // where it is now.
  useEffect(() => {
    const onResize = () => {
      const bar = main.current?.parentElement?.querySelector<HTMLElement>(':scope > .bar')
      if (bar && last.current) last.current = { ...last.current, y: bar.offsetTop, x: bar.offsetLeft + bar.offsetWidth / 2 }
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [main])
}

const samePlace = (a: Place, b: Place) => a.centred === b.centred && a.placed === b.placed && a.beside === b.beside

// useColumnGlide moves the conversation (or the board) between the middle
// of the stage and its column beside a panel. The column takes its new
// width at once, so its text wraps once, and then slides from where its
// text was to where it is, by transform, while the panel comes in or
// goes.
function useColumnGlide(main: RefObject<HTMLElement | null>, beside: boolean) {
  const last = useRef<{ beside: boolean; left: number } | null>(null)
  useLayoutEffect(() => {
    const col = main.current?.querySelector<HTMLElement>(':scope > .log, :scope > .home:not(.leaving)')
    const inner = col?.firstElementChild as HTMLElement | null | undefined
    if (!col || !inner) {
      last.current = null
      return
    }
    // The column's own transform is left out: the left it was laid out at.
    const left = inner.getBoundingClientRect().left - translateX(col)
    const prev = last.current
    last.current = { beside, left }
    if (!prev || prev.beside === beside || Math.abs(prev.left - left) < 2 || reducedMotion()) return
    col.getAnimations().forEach((a) => a.id === 'column' && a.cancel())
    const a = col.animate([{ transform: `translateX(${prev.left - left}px)`, opacity: 0.4 }, { transform: 'translateX(0)', opacity: 1 }], { duration: moveMs, easing: moveEase })
    a.id = 'column'
  })
  // A resize moves the column without a render: start from there.
  useEffect(() => {
    const onResize = () => {
      const inner = main.current?.querySelector<HTMLElement>(':scope > .log > :first-child, :scope > .home:not(.leaving) > :first-child')
      const col = inner?.parentElement
      if (last.current && inner && col) last.current.left = inner.getBoundingClientRect().left - translateX(col)
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [main])
}

function translateX(el: HTMLElement) {
  const t = getComputedStyle(el).transform
  return t && t !== 'none' ? new DOMMatrixReadOnly(t).m41 : 0
}

// useMorph is true for the length of the bar's morph after voice comes or
// goes, so the stage and the bar animate their size for it and for
// nothing else. It's worked out as it renders, so the class arrives with
// the change it animates.
function useMorph(voice: boolean) {
  const [s, set] = useState({ voice, on: false })
  if (s.voice !== voice) set({ voice, on: true })
  useEffect(() => {
    if (!s.on) return
    const t = setTimeout(() => set((x) => ({ ...x, on: false })), moveMs)
    return () => clearTimeout(t)
  }, [s])
  return s.voice !== voice || s.on
}

// useBoardTabOrder keeps Tab in the order things are seen while the
// composer sits in the home board: the header, the composer, then the
// board's sections. The composer is the stage's sibling, after it in the
// page, so on its own Tab would leave the composer for the page's end and
// reach it only after the sections.
function useBoardTabOrder(main: RefObject<HTMLElement | null>, on: boolean) {
  useEffect(() => {
    const app = main.current?.closest<HTMLElement>('.app')
    if (!on || !app) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Tab' || e.altKey || e.ctrlKey || e.metaKey || e.defaultPrevented) return
      const bar = app.querySelector<HTMLElement>(':scope > .bar')
      const more = main.current?.querySelector<HTMLElement>('.home-more')
      const head = app.querySelector<HTMLElement>(':scope > .header')
      if (!bar || !more) return
      const inBar = focusables(bar)
      const inMore = focusables(more)
      const t = e.target as HTMLElement
      const step = boardTab({ bar: inBar.indexOf(t), barCount: inBar.length, more: inMore.indexOf(t), moreCount: inMore.length }, e.shiftKey)
      if (!step) return
      if (step === 'past-bar') {
        // Past the end of the page: the composer is skipped this once,
        // and the browser takes Tab on from there.
        bar.inert = true
        setTimeout(() => (bar.inert = false))
        return
      }
      const to = step === 'more' ? inMore[0] : step === 'bar' ? inBar[inBar.length - 1] : head ? focusables(head).pop() : undefined
      if (!to) return
      e.preventDefault()
      to.focus()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [main, on])
}
