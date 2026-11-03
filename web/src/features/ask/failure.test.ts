import { describe, expect, it } from 'vitest'
import { failureView, waitLeft } from './failure'

// What a failed run's card offers (WP-10, Appendix B.4).

describe('failureView', () => {
  it('offers Try again when the server says it may work', () => {
    const v = failureView({ reason: 'model_unavailable', retryable: true, partial: true, detail: 'No answer after 4 tries.' }, 'overloaded')
    expect(v).toMatchObject({ title: 'Alexa’s model isn’t answering', body: 'No answer after 4 tries.', retry: true, repair: false, partial: true })
  })
  it('offers Fix conversation, and Try again after it, for unreadable input', () => {
    const v = failureView({ reason: 'invalid_input', retryable: false }, 'An attachment couldn’t be read.')
    expect(v).toMatchObject({ retry: true, repair: true, body: 'An attachment couldn’t be read.' })
  })
  it('offers nothing to retry when retrying won’t help', () => {
    expect(failureView({ reason: 'loop_blocked', retryable: false }, '').retry).toBe(false)
  })
  it('treats an unknown reason as internal', () => {
    const v = failureView({ reason: 'gremlins' as never, retryable: false }, 'Huh')
    expect(v.title).toBe('Something went wrong')
    expect(v.reason).toBe('internal')
  })
  it('doesn’t repeat the title as the body', () => {
    expect(failureView({ reason: 'stalled', retryable: true }, 'That took too long').body).toBe('')
    expect(failureView({ reason: 'rate_limited', retryable: true }, 'Too many requests.').body).toBe('')
    expect(failureView({ reason: 'invalid_input', retryable: false }, 'Alexa couldn\'t read something you sent.').body).toBe('')
  })
  it('keeps a body that says more than the title', () => {
    expect(failureView({ reason: 'rate_limited', retryable: true }, 'Too many requests from this account today.').body).toBe('Too many requests from this account today.')
  })
  it('keeps a legacy error’s text', () => {
    expect(failureView(undefined, 'The model didn’t answer in time.')).toMatchObject({ title: 'Something went wrong', body: 'The model didn’t answer in time.', retry: false })
  })
  it('keeps a retry time only when there is one', () => {
    expect(failureView({ reason: 'rate_limited', retryable: true, retryAt: 0 }, '').retryAt).toBeUndefined()
    expect(failureView({ reason: 'rate_limited', retryable: true, retryAt: 5000 }, '').retryAt).toBe(5000)
  })
})

describe('waitLeft', () => {
  it('counts down in minutes and seconds', () => {
    expect(waitLeft(10_000, 2_500)).toBe('0:08')
    expect(waitLeft(100_000, 0)).toBe('1:40')
  })
  it('is empty once the wait is over', () => {
    expect(waitLeft(1000, 1000)).toBe('')
    expect(waitLeft(undefined, 0)).toBe('')
  })
})
