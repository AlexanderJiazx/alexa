import type { Failure, FailureReason } from '../../api/progress'

// What a failed run's card says (Appendix B.4): a plain title for why, the
// server's user-safe detail, and which ways forward make sense. Pure, so
// it's tested (failure.test.ts).

export type FailureView = {
  title: string
  body: string
  retry: boolean // Try again relaunches from the last good point
  repair: boolean // Fix conversation sets aside what the model can't read
  retryAt?: number
  partial: boolean
  reason?: FailureReason
}

const titles: Record<FailureReason, string> = {
  model_unavailable: 'Alexa’s model isn’t answering',
  rate_limited: 'Too many requests for now',
  stalled: 'That took too long',
  context_overflow: 'The conversation got too long',
  empty_response: 'Alexa came back with nothing',
  loop_blocked: 'Alexa was going round in circles',
  invalid_input: 'Alexa couldn’t read something you sent',
  tool_crashed: 'A tool stopped working',
  internal: 'Something went wrong',
}

// bare is text for comparing: lower case, plain apostrophes, no closing
// punctuation.
const bare = (s: string) =>
  s
    .toLowerCase()
    .replace(/[’‘]/g, "'")
    .replace(/[.!…\s]+$/, '')
    .trim()

// says reports whether the title already says what the body would: the
// same words, or the start of them ("Too many requests." under "Too many
// requests for now").
const says = (title: string, body: string) => bare(title).startsWith(bare(body))

export function failureView(f: Failure | undefined, text: string): FailureView {
  if (!f) return { title: 'Something went wrong', body: text, retry: false, repair: false, partial: false }
  const reason = f.reason in titles ? f.reason : 'internal'
  const body = (f.detail || text || '').trim()
  return {
    title: titles[reason],
    // The title already says it when the server has nothing more.
    body: says(titles[reason], body) ? '' : body,
    retry: !!f.retryable || reason === 'invalid_input',
    repair: reason === 'invalid_input',
    retryAt: f.retryAt && f.retryAt > 0 ? f.retryAt : undefined,
    partial: !!f.partial,
    reason,
  }
}

// waitLeft is the countdown before Try again makes sense, "0:07", or ''.
export function waitLeft(retryAt: number | undefined, now: number) {
  if (!retryAt) return ''
  const s = Math.ceil((retryAt - now) / 1000)
  if (s <= 0) return ''
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}
