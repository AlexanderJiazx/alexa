import { isFollowing, setFollowing } from '../../state/exchange'
import { useSession } from '../../state/selectors'
import { cx } from '../../ui/cx'
import { Toggle } from '../../ui/Toggle'
import './caption.css'

// FollowChip is the panel head's "Follow Alexa" switch: on, Alexa's work
// moves the screen (to the browser while it browses, to a new report);
// off, the screen stays as it is until the next request. Panels put it in
// their head, before Close.
export function FollowChip({ className }: { className?: string }) {
  const on = useSession(isFollowing)
  return <FollowChipView on={on} onChange={setFollowing} className={className} />
}

type ViewProps = { on: boolean; onChange: (on: boolean) => void; className?: string }

// The words say what the switch does, on or off; the switch says which it
// is. The whole chip is its label, so the words toggle it too, and the
// switch's name holds the words, so "click Follow" works by voice.
export function FollowChipView({ on, onChange, className }: ViewProps) {
  return (
    <label className={cx('follow-chip', className)} title={on ? 'The screen follows Alexa’s work' : 'The screen stays as it is'}>
      <span className="fc-label" aria-hidden="true">
        Follow<span className="fc-more"> Alexa</span>
      </span>
      <Toggle checked={on} onChange={onChange} label="Follow Alexa" />
    </label>
  )
}
