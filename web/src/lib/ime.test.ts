import { describe, expect, it } from 'vitest'
import { fromIme } from './ime'

// FH-12: in Safari, the Enter that picks an IME candidate sent the message.
// Safari reports that Enter after compositionend, so isComposing is false;
// only keyCode 229 gives it away.
describe('fromIme', () => {
  it('takes the Enter that picks a candidate for the input method’s', () => {
    expect(fromIme({ isComposing: true, keyCode: 13 })).toBe(true)
    expect(fromIme({ isComposing: false, keyCode: 229 })).toBe(true)
  })
  it('leaves a plain Enter to the app', () => {
    expect(fromIme({ isComposing: false, keyCode: 13 })).toBe(false)
  })
})
