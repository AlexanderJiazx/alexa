import { FailureCard, useIsLatest } from '../../ask/FailureCard'
import type { EntryProps } from '../registry'

// ErrorEntry is a run that went wrong: a card that says why, from
// entry.data.failure, with Try again while it's the latest thing that
// happened (WP-10, Appendix B.4).
export function ErrorEntry({ entry: e, mode }: EntryProps) {
  const latest = useIsLatest(e.id)
  return <FailureCard entry={e} live={mode === 'now' && latest} />
}
