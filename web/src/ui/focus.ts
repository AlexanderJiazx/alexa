// Keyboard helpers shared by the primitives. The decisions are pure
// functions so they test without a DOM; the DOM side is a thin wrapper.

// moveIndex is where a roving focus goes for a key in a list of count items
// (menus, segmented controls, tabs). It wraps at the ends, as WAI-ARIA menus
// and radio groups do. null means the key isn't a moving key.
export function moveIndex(key: string, current: number, count: number, orientation: 'vertical' | 'horizontal' | 'both' = 'both'): number | null {
  if (count <= 0) return null
  const next = orientation !== 'horizontal' ? 'ArrowDown' : 'ArrowRight'
  const prev = orientation !== 'horizontal' ? 'ArrowUp' : 'ArrowLeft'
  const alsoNext = orientation === 'both' ? 'ArrowRight' : ''
  const alsoPrev = orientation === 'both' ? 'ArrowLeft' : ''
  if (key === next || key === alsoNext) return current < 0 ? 0 : (current + 1) % count
  if (key === prev || key === alsoPrev) return current < 0 ? count - 1 : (current - 1 + count) % count
  if (key === 'Home') return 0
  if (key === 'End') return count - 1
  return null
}

// trapIndex is where Tab goes inside a focus trap of count focusable
// elements: from the last forward to the first, from the first back to the
// last. null means the browser's own Tab is fine (it stays inside).
export function trapIndex(current: number, count: number, back: boolean): number | null {
  if (count === 0) return null
  if (current < 0) return back ? count - 1 : 0
  if (back && current === 0) return count - 1
  if (!back && current === count - 1) return 0
  return null
}

const focusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type=hidden])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

// focusables lists what Tab can reach inside el, in order.
export function focusables(el: HTMLElement): HTMLElement[] {
  return Array.from(el.querySelectorAll<HTMLElement>(focusableSelector)).filter((x) => !x.hasAttribute('inert') && x.getClientRects().length > 0)
}

// trapTab keeps Tab and Shift+Tab inside el. Call it from a keydown handler.
export function trapTab(e: KeyboardEvent | React.KeyboardEvent, el: HTMLElement) {
  if (e.key !== 'Tab') return
  const list = focusables(el)
  const i = trapIndex(list.indexOf(document.activeElement as HTMLElement), list.length, e.shiftKey)
  if (i === null) return
  e.preventDefault()
  list[i]?.focus()
}
