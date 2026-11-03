import { describe, expect, it, vi } from 'vitest'
import { ifUnsent } from './Suggestions'

// A follow-up chip steps aside as it's tapped, and comes back when its
// message couldn't be sent, so it can be tapped again (FE-5).

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('ifUnsent', () => {
  it('calls back when the send failed or threw', async () => {
    const back = vi.fn()
    ifUnsent(Promise.resolve(false), back)
    ifUnsent(Promise.reject(new Error('offline')), back)
    await flush()
    expect(back).toHaveBeenCalledTimes(2)
  })
  it('stays quiet when it went, or when there was nothing to wait for', async () => {
    const back = vi.fn()
    ifUnsent(Promise.resolve(true), back)
    ifUnsent(undefined, back)
    await flush()
    expect(back).not.toHaveBeenCalled()
  })
})
