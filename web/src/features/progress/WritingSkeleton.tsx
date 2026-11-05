import { Skeleton } from '../../ui/Skeleton'
import type { Writing } from './phase'

// WritingSkeleton holds the place of what Alexa is writing (the writing
// phase): a reply's lines, a question and its answers, or a report's card,
// in the shape each will arrive in.
export function WritingSkeleton({ target }: { target: Writing }) {
  if (target === 'report')
    return (
      <div className="pg-skel pg-skel-report" aria-hidden="true">
        <Skeleton width={36} height={36} radius="sm" />
        <span className="pg-skel-lines">
          <Skeleton width="58%" height={14} />
          <Skeleton width="34%" height={12} />
        </span>
      </div>
    )
  if (target === 'ask')
    return (
      <div className="pg-skel pg-skel-ask" aria-hidden="true">
        <Skeleton width="72%" height={16} />
        <span className="pg-skel-row">
          <Skeleton width={96} height={32} radius="full" />
          <Skeleton width={120} height={32} radius="full" />
          <Skeleton width={80} height={32} radius="full" />
        </span>
      </div>
    )
  return (
    <div className="pg-skel pg-skel-say" aria-hidden="true">
      <Skeleton lines={3} />
    </div>
  )
}
