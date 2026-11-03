// keepFocusNear is for a control about to disappear with its row (a
// delete): once the row has gone, the same control in the row that took its
// place (or the one above) takes the focus, so the keyboard isn't dropped
// back at the top of the page. Call it before the row is removed.
const focusable = 'button:not([disabled]), a[href], input:not([disabled]), textarea:not([disabled])'

export function keepFocusNear(from: Element | null = typeof document === 'undefined' ? null : document.activeElement) {
  const row = from?.closest('li')
  const list = row?.parentElement
  if (!from || !row || !list) return
  const at = [...row.querySelectorAll(focusable)].indexOf(from)
  const next = row.nextElementSibling
  const prev = row.previousElementSibling
  requestAnimationFrame(() => {
    if (row.isConnected && !row.hidden && document.activeElement === from) return
    const into = [next, prev].find((r) => r?.isConnected)
    const controls = into ? [...into.querySelectorAll<HTMLElement>(focusable)] : []
    const target = controls[at] ?? controls[0] ?? list.closest('dialog, [role=tabpanel]')?.querySelector<HTMLElement>(focusable)
    target?.focus()
  })
}
