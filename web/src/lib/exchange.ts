import { useCallback, useEffect, useRef, useState } from 'react'
import type { Entry } from './api'

// The main screen shows the current exchange: from the latest message that
// set Alexa to work while it was idle, up to now. Everything before it is
// in Reflect. A finished exchange you've seen is put away when you come
// back to it later, and you can put it away yourself (the mark, top left).

// How long after it finished an exchange you've seen stays on screen.
const fresh = 30 * 60e3

// exchangeStart returns where the current exchange begins in the feed.
export function exchangeStart(feed: Entry[]) {
  for (let i = feed.length - 1; i >= 0; i--) if (feed[i].data?.start) return i
  // Entries from before exchanges were marked.
  for (let i = feed.length - 1; i >= 0; i--) if (feed[i].kind === 'user' || feed[i].data?.scheduled) return i
  return 0
}

// current returns the exchange the main screen shows. A scheduled job that
// hasn't had anything to say (only its note and steps so far) stays off it,
// and the exchange before it stays in view.
export function current(feed: Entry[]) {
  let end = feed.length
  for (;;) {
    const start = exchangeStart(feed.slice(0, end))
    const ex = feed.slice(start, end)
    const quiet = ex[0]?.data?.scheduled && ex.every((e) => e.kind === 'step' || e.kind === 'note')
    if (!quiet) return ex
    if (start === 0) return []
    end = start
  }
}

const read = (key: string) => {
  try {
    return Number(localStorage.getItem(key)) || 0
  } catch {
    return 0
  }
}

const write = (key: string, id: number) => {
  try {
    localStorage.setItem(key, String(id))
  } catch {
    /* storage unavailable */
  }
}

// useExchange returns the entries the main screen shows, and clear, which
// puts them away. Which entries were put away and which were seen is kept
// per device.
export function useExchange(feed: Entry[], idle: boolean, userId: number, loaded: boolean) {
  const [hidden, setHidden] = useState(0)
  const seen = useRef(0)
  const shown = current(feed)
  const now = useRef({ feed, shown, idle })
  now.current = { feed, shown, idle }
  const last = feed[feed.length - 1]

  useEffect(() => {
    setHidden(read(`alexa-hidden-${userId}`))
    seen.current = read(`alexa-seen-${userId}`)
  }, [userId])

  const clear = useCallback(() => {
    const f = now.current.feed
    const id = f.length ? f[f.length - 1].id : 0
    setHidden(id)
    write(`alexa-hidden-${userId}`, id)
  }, [userId])

  // What's on screen while the app is in view counts as seen.
  const markSeen = useCallback(() => {
    const f = now.current.feed
    const id = f.length ? f[f.length - 1].id : 0
    if (document.visibilityState !== 'visible' || id === seen.current) return
    seen.current = id
    write(`alexa-seen-${userId}`, id)
  }, [userId])

  // Coming back to a finished exchange you've seen, a while after it
  // finished, you're greeted afresh.
  const tidy = useCallback(() => {
    const { shown: ex, idle } = now.current
    const end = ex[ex.length - 1]
    if (idle && end && end.id <= seen.current && Date.now() - end.updatedAt > fresh) clear()
  }, [clear])

  useEffect(() => {
    if (!loaded) return
    tidy()
    markSeen()
    const onVis = () => {
      if (document.visibilityState !== 'visible') return
      tidy()
      markSeen()
    }
    document.addEventListener('visibilitychange', onVis)
    const timer = setInterval(tidy, 5 * 60e3)
    return () => {
      document.removeEventListener('visibilitychange', onVis)
      clearInterval(timer)
    }
  }, [loaded, tidy, markSeen])

  useEffect(() => {
    if (loaded) markSeen()
  }, [loaded, last?.id, markSeen])

  // A mark from before the conversation's entries were renumbered (a new
  // server) is ignored.
  const cut = last && hidden > last.id ? 0 : hidden
  return { entries: shown.filter((e) => e.id > cut), clear }
}
