// The composer's text field, for the few places that hand the user over to
// it: a letter typed on an ask card, "Type something else", and Change on a
// confirm card. The Composer registers its field while it's mounted.

let field: HTMLTextAreaElement | null = null
let filler: ((text: string) => void) | null = null

export function registerComposer(el: HTMLTextAreaElement | null) {
  field = el
}

// registerPrefill is how the Composer takes text put in it from elsewhere:
// it sets its own state, so nothing has to fake typing.
export function registerPrefill(fn: ((text: string) => void) | null) {
  filler = fn
}

// prefillComposer puts text in the composer for the user to finish and
// send themselves, with the caret at its end. It never sends anything.
export function prefillComposer(text: string) {
  if (!filler) return false
  filler(text)
  return true
}

// focusComposer puts the caret at the end of the composer. Called during a
// keydown, the key that was pressed lands in it.
export function focusComposer() {
  if (!field || field.disabled) return false
  field.focus({ preventScroll: true })
  const n = field.value.length
  field.setSelectionRange(n, n)
  return true
}

export const composerFocused = () => !!field && document.activeElement === field

export const composerEmpty = () => !field || field.value.trim() === ''
