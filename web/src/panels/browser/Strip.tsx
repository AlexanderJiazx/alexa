import type { Needs } from '../../api/types'
import { cx } from '../../ui/cx'
import { Hand } from '../../ui/icons'
import { Mark } from '../../ui/Mark'

type Props = { control: boolean; needs: Needs | null; working: boolean }

// Strip is the line over the page that says who has the browser and what
// for. In control it stays for as long as you have it, with what Alexa
// asked you to do (IX-F24); before that it carries Alexa's request.
export function Strip({ control, needs, working }: Props) {
  const asked = needs?.kind === 'takeover' ? needs.message : ''
  if (control)
    return (
      <div className="bp-strip in-control" role="status">
        <Hand size={16} />
        <p>
          <b>You’re in control.</b> {asked ? <span>{asked}</span> : <span>{working ? 'Alexa is waiting until you hand back.' : 'Alexa won’t touch the browser until you’re done.'}</span>}{' '}
          {/* With a keyboard, how to get out of the page (LiveScreen). */}
          <span className="kbd-hint bp-strip-hint">Esc twice leaves the page.</span>
        </p>
      </div>
    )
  if (needs?.kind === 'takeover' || needs?.kind === 'login')
    return (
      <div className={cx('bp-strip', 'asks')} role="status">
        <Mark state="needs" size="sm" />
        <p>
          <b>Alexa needs you.</b> <span>{needs.kind === 'login' ? `Sign in to ${needs.site || 'this site'}, or take over and do it yourself.` : needs.message}</span>
        </p>
      </div>
    )
  return null
}
