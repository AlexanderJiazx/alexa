import { cx } from './cx'

export type Presence = 'idle' | 'working' | 'waiting' | 'needs'

const stateClass: Record<Presence, string> = { idle: '', working: 'busy', waiting: 'wait', needs: 'needs' }

// Mark is the dot: Alexa's presence everywhere it appears. Idle is still
// ink; working breathes with an accent orbit; waiting (while the user is in
// control) breathes slowly; needs-you is an accent pulse. The bars belong to
// voice only.
export function Mark({ state = 'idle', size = 'md', draw, label }: { state?: Presence; size?: 'sm' | 'md' | 'lg'; draw?: boolean; label?: string }) {
  return <span className={cx('mark', stateClass[state], size !== 'md' && size, draw && 'draw')} role={label ? 'img' : undefined} aria-label={label} aria-hidden={label ? undefined : true} />
}
