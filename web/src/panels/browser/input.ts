// Take-over input: turning the user's pointer, wheel and touch into the
// mouse events the server plays into the page (internal/browser
// HumanInput). Pure, so it's tested without a browser.

export type Size = { w: number; h: number }
export type Rect = { left: number; top: number; width: number; height: number }
export type PagePoint = { x: number; y: number }

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))

// toPage maps a point on the screen to page pixels. A drag that leaves the
// screen (the pointer is captured) stays on the page's edge, so a slider
// pulled past its end ends there.
export function toPage(clientX: number, clientY: number, r: Rect, size: Size): PagePoint {
  const x = ((clientX - r.left) / (r.width || 1)) * size.w
  const y = ((clientY - r.top) / (r.height || 1)) * size.h
  return { x: Math.round(clamp(x, 0, size.w - 1)), y: Math.round(clamp(y, 0, size.h - 1)) }
}

// The DOM numbers a mouse's buttons one way in `button` (0 left, 1 middle,
// 2 right, 3 back, 4 forward) and another in the `buttons` mask (1 left,
// 2 right, 4 middle, 8 back, 16 forward). The server takes the mask as is.
const bits = [1, 4, 2, 8, 16]
export const buttonBit = (button: number) => bits[button] ?? 0

// buttonName is the server's name for a button that presses in the page.
// Back and forward don't: they go back and forward instead.
export function buttonName(button: number): 'left' | 'middle' | 'right' | null {
  return button === 0 ? 'left' : button === 1 ? 'middle' : button === 2 ? 'right' : null
}

export const historyFor = (button: number): 'back' | 'forward' | null => (button === 3 ? 'back' : button === 4 ? 'forward' : null)

// chord says whether a pointermove is really a second button going down or
// up while another is held: pointer events report those as moves, with
// `button` set to the one that changed.
export function chord(button: number, buttons: number): 'down' | 'up' | null {
  if (button < 0 || button === undefined) return null
  return buttons & buttonBit(button) ? 'down' : 'up'
}

type Mods = { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }

// modifiers is the CDP modifier mask: Alt 1, Control 2, Meta 4, Shift 8.
export const modifiers = (e: Mods) => (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0)

// lineHeight is what a wheel "line" is worth in pixels, as Chrome counts it.
const lineHeight = 40

// wheelDelta turns a wheel event's deltas into page pixels: lines and pages
// (Firefox) become pixels, and the result is scaled from the screen to the
// page, so the page moves under the fingers as far as they did.
export function wheelDelta(dx: number, dy: number, mode: number, size: Size, scale: number) {
  const unit = mode === 1 ? lineHeight : mode === 2 ? size.h : 1
  return { dx: dx * unit * scale, dy: dy * unit * scale }
}

// ---------- touch ----------

// A finger on the page is a tap (a click), a swipe (a scroll), or, held
// still for holdMs first, a drag: the mouse goes down where it's held and
// follows the finger until it lifts (B05). That's how a slider or a "slide
// to verify" works from a phone.
export const slop = 8
export const holdMs = 450

export type TouchPhase = 'pending' | 'scroll' | 'drag'
export type Touch = { phase: TouchPhase; x0: number; y0: number; x: number; y: number }

// What a touch does, in screen pixels: the caller maps points to the page.
export type TouchOut =
  | { kind: 'tap'; x: number; y: number }
  | { kind: 'scroll'; x: number; y: number; dx: number; dy: number }
  | { kind: 'down'; x: number; y: number }
  | { kind: 'move'; x: number; y: number }
  | { kind: 'up'; x: number; y: number }

export function touchStart(x: number, y: number): Touch {
  return { phase: 'pending', x0: x, y0: y, x, y }
}

export function touchMove(t: Touch, x: number, y: number): [Touch, TouchOut[]] {
  if (t.phase === 'drag') return [{ ...t, x, y }, [{ kind: 'move', x, y }]]
  if (t.phase === 'pending' && Math.hypot(x - t.x0, y - t.y0) <= slop) return [t, []]
  // A swipe scrolls the page the way the finger went: up shows what's below.
  const out: TouchOut = { kind: 'scroll', x, y, dx: t.x - x, dy: t.y - y }
  return [{ ...t, phase: 'scroll', x, y }, [out]]
}

// touchHold is the long press firing: a finger still within slop starts a
// drag where it is.
export function touchHold(t: Touch): [Touch, TouchOut[]] {
  if (t.phase !== 'pending') return [t, []]
  return [{ ...t, phase: 'drag' }, [{ kind: 'down', x: t.x, y: t.y }]]
}

export function touchEnd(t: Touch, x: number, y: number): TouchOut[] {
  if (t.phase === 'pending') return [{ kind: 'tap', x: t.x0, y: t.y0 }]
  if (t.phase === 'drag') return [{ kind: 'up', x, y }]
  return []
}

// touchCancel: the system took the touch (a call, an edge swipe). A drag
// still lets go of the mouse, so nothing in the page stays held.
export function touchCancel(t: Touch): TouchOut[] {
  return t.phase === 'drag' ? [{ kind: 'up', x: t.x, y: t.y }] : []
}

// ---------- zoom ----------

// A 1280px page on a phone is a quarter of its size, its links a few pixels
// tall. In control, two fingers zoom and pan the picture (only the picture:
// the page itself doesn't change), and points map back through the zoom
// because they're read off the picture's own box.

// Zoom is the picture's scale, then its offset within the screen, in CSS
// pixels.
export type Zoom = { k: number; x: number; y: number }
export type Point = { x: number; y: number }
export const noZoom: Zoom = { k: 1, x: 0, y: 0 }
export const maxZoom = 4

const mid = (a: Point, b: Point) => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 })

// pinchZoom is the zoom while two fingers that went down at a0 and b0 (on
// a picture zoomed z0) are at a and b: scaled by how far apart they've
// moved, and moved so the spot that was between them stays between them.
// The picture always covers the screen (w by h), and never shrinks below
// its size. Points are relative to the screen's top left.
export function pinchZoom(z0: Zoom, a0: Point, b0: Point, a: Point, b: Point, w: number, h: number): Zoom {
  const d0 = Math.hypot(b0.x - a0.x, b0.y - a0.y) || 1
  const k = clamp((z0.k * Math.hypot(b.x - a.x, b.y - a.y)) / d0, 1, maxZoom)
  const m0 = mid(a0, b0)
  const m = mid(a, b)
  // The spot on the picture that was under the fingers.
  const cx = (m0.x - z0.x) / z0.k
  const cy = (m0.y - z0.y) / z0.k
  return { k, x: clamp(m.x - cx * k, w - w * k, 0), y: clamp(m.y - cy * k, h - h * k, 0) }
}
