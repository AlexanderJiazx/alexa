// How Alexa's pointer travels: a reach along a slight curve, timed so the
// server's own wait before acting (internal/browser, the same formula)
// ends as the pointer arrives.

export type Point = { x: number; y: number }

// glide is how long the pointer takes to travel dist pixels of the page; the
// server waits as long before acting, so the page reacts as it arrives.
export function glide(dist: number) {
  return dist < 1 ? 0 : Math.min(220 + 0.3 * dist, 700)
}

// A reach: slow start, quick middle, gentle landing (minimum jerk).
export const reach = (t: number) => t * t * t * (10 - 15 * t + 6 * t * t)

// Path is one glide: where it starts and ends, and the control point that
// bends it a little to one side, as a hand would.
export type Path = { from: Point; to: Point; c: Point }

// pathFor bends the path left or right in turn (side), by at most 56px.
export function pathFor(from: Point, to: Point, side: 1 | -1): Path {
  const dx = to.x - from.x
  const dy = to.y - from.y
  const dist = Math.hypot(dx, dy)
  const bend = Math.min(dist * 0.16, 56) * side
  const c = { x: from.x + dx / 2 - (dy / (dist || 1)) * bend, y: from.y + dy / 2 + (dx / (dist || 1)) * bend }
  return { from, to, c }
}

// pointAt is where the pointer is at time t (0 to 1) of the glide.
export function pointAt({ from, to, c }: Path, t: number): Point {
  const e = reach(Math.min(1, Math.max(0, t)))
  const u = 1 - e
  return { x: u * u * from.x + 2 * u * e * c.x + e * e * to.x, y: u * u * from.y + 2 * u * e * c.y + e * e * to.y }
}
