// fromIme says whether a key press belongs to an input method (Japanese,
// Chinese, Korean, dictation…) rather than being a key of its own: any key
// while a composition is open, and the Enter that picks a candidate, which
// Safari sends after the composition has ended, with keyCode 229. Pass the
// native event.
export const fromIme = (e: Pick<KeyboardEvent, 'isComposing' | 'keyCode'>) => e.isComposing || e.keyCode === 229
