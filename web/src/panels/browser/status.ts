// What the live view says about itself: whether the picture is live, and
// how long to wait before trying the socket again. Pure, so it's tested
// without a browser.

// SocketState is the live socket: never opened yet, open, or closed after
// having been open (and about to try again).
export type SocketState = 'connecting' | 'open' | 'closed'

// LiveStatus is what the Live pill shows.
//   connecting:   no picture yet
//   live:         frames are arriving
//   reconnecting: the socket dropped; the picture is the last one we had
//   stale:        the socket is up, but something should have changed the
//                 page and no frame came for staleAfter
//   asleep:       the server put the browser to sleep (B10); it wakes when
//                 Alexa needs it, or on Wake
export type LiveStatus = 'connecting' | 'live' | 'reconnecting' | 'stale' | 'asleep'

export type StatusInput = {
  socket: SocketState
  asleep: boolean
  hasFrame: boolean
  stale: boolean
}

export function liveStatus({ socket, asleep, hasFrame, stale }: StatusInput): LiveStatus {
  if (asleep) return 'asleep'
  if (socket === 'closed') return 'reconnecting'
  if (socket === 'connecting' || !hasFrame) return 'connecting'
  return stale ? 'stale' : 'live'
}

// staleAfter is how long the view waits for a frame after something that
// should change the page (Alexa acting, the user clicking or typing).
export const staleAfter = 5000

// isStale says whether an expectation set at since has gone unmet for too
// long. since is null when nothing is expected.
export function isStale(since: number | null, now: number) {
  return since !== null && now - since > staleAfter
}

// retryDelay is how long to wait before the attempt-th reconnect (from 1):
// half a second, doubling, at most ten, with a little jitter so many tabs
// don't all knock at once. rnd is Math.random, passed in for tests.
export function retryDelay(attempt: number, rnd = Math.random) {
  const base = Math.min(10000, 500 * 2 ** Math.max(0, attempt - 1))
  return Math.round(base * (0.85 + 0.3 * rnd()))
}

// reconnectDelay is the wait before the attempt-th reconnect after a close
// with code. A browser that restarted (closeRestarted) comes straight back
// once; if it goes again before the socket proved itself, the client backs
// off like any other drop, so a browser that dies as it starts never spins
// the client and the server in a loop.
export function reconnectDelay(attempt: number, code: number, rnd = Math.random) {
  return code === closeRestarted && attempt <= 1 ? 0 : retryDelay(attempt, rnd)
}

// stableAfter is how long a socket must stay open, if no frame comes, before
// it counts as healthy and the backoff starts again from the beginning.
export const stableAfter = 5000

// Close codes the live socket acts on. restarted: the server closed it
// because the browser behind it was replaced (a crash, B02), so it comes
// straight back. revoked: signed out; the app's own socket handles that,
// so this one stops.
export const closeRestarted = 1012
export const closeRevoked = 4401

// statusLabel is the pill's words.
export const statusLabel: Record<LiveStatus, string> = {
  connecting: 'Connecting',
  live: 'Live',
  reconnecting: 'Reconnecting',
  stale: 'Stale',
  asleep: 'Asleep',
}
