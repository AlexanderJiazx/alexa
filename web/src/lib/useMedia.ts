import { useSyncExternalStore } from 'react'

type Media = { subscribe: (fn: () => void) => () => void; matches: () => boolean }
const made = new Map<string, Media>()

// media is a query's subscribe and read, made once per query: a subscribe
// made afresh on every render would have React unsubscribe and subscribe
// again each time. The query list itself is only asked for once it's
// needed, so a render without a browser (the tests) never touches it.
export function media(query: string): Media {
  let m = made.get(query)
  if (!m) {
    let list: MediaQueryList | null = null
    const get = () => (list ??= matchMedia(query))
    m = {
      subscribe: (fn) => {
        const l = get()
        l.addEventListener('change', fn)
        return () => l.removeEventListener('change', fn)
      },
      matches: () => get().matches,
    }
    made.set(query, m)
  }
  return m
}

// useMedia says whether a media query matches, and follows it as it
// changes (a window resized, a phone turned).
export function useMedia(query: string) {
  const m = media(query)
  return useSyncExternalStore(m.subscribe, m.matches, () => false)
}
