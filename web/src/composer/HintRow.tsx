import { cx } from '../ui/cx'

// HintRow is the line of key hints just under the composer, on screens with
// a keyboard: what Enter does, and the rest. It shows once something is
// typed in the field (not just for focusing it), for the first few
// messages.
export function HintRow({ show, working }: { show: boolean; working: boolean }) {
  return (
    <p className={cx('hint-row', show && 'on')} aria-hidden="true">
      <span>
        <b>↵</b> {working ? 'add to the task' : 'send'}
      </span>
      <span>
        <b>⇧↵</b> new line
      </span>
      {working && (
        <span>
          <b>⌘↵</b> stop and do this instead
        </span>
      )}
    </p>
  )
}
