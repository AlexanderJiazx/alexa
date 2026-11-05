import type { ReactNode } from 'react'
import type { Action as A } from '../../../api/blocks'
import { openReport } from '../../../state/actions'
import { Button, buttonClass } from '../../../ui/Button'
import { Check, External, Report } from '../../../ui/icons'
import { useBlockIndex, useBlocks } from '../context'

// safeURL keeps only web links: the server allows nothing else, and a
// block must never carry a javascript: or file: link.
export function safeURL(u?: string) {
  if (!u) return undefined
  try {
    const p = new URL(u)
    return p.protocol === 'https:' || p.protocol === 'http:' ? p.href : undefined
  } catch {
    return undefined
  }
}

// Action is a block's one button. A reply sends its text as the user's
// visible message (never a hidden turn); open opens a link; report opens a
// report. After the exchange moves on, a reply is disabled and the one
// that was chosen says so.
export function Action({ action, item = 0, primary }: { action: A; item?: number; primary?: boolean }) {
  const ctx = useBlocks()
  const block = useBlockIndex()
  if (action.do === 'reply') {
    const chosen = ctx.sent === action.text || (ctx.frozen && ctx.answer?.trim() === action.text.trim())
    if (chosen)
      return (
        <span className="b-chosen" role="status">
          <Check size={14} />
          {ctx.sent && !ctx.answer ? 'Sent' : 'Chosen'}
        </span>
      )
    // A past choice doesn't shout: frozen replies all sit back as
    // secondary buttons, the pick's included. The label stays the button's
    // name; the tooltip (and its description) says what it sends.
    return (
      <Button size="sm" variant={primary && !ctx.frozen ? 'primary' : 'secondary'} disabled={ctx.frozen || !!ctx.sent} onClick={() => ctx.reply(action.text, { block, item })} title={`Sends “${action.text}”`}>
        {action.label}
      </Button>
    )
  }
  if (action.do === 'open') {
    const href = safeURL(action.url)
    if (!href) return null
    return (
      <a className={buttonClass(primary ? 'primary' : 'secondary', 'sm')} href={href} target="_blank" rel="noopener noreferrer">
        {action.label || 'Open'}
        <External size={14} />
      </a>
    )
  }
  if (action.do === 'report')
    return (
      <Button size="sm" variant={primary ? 'primary' : 'secondary'} icon={<Report size={16} />} onClick={() => openReport(String(action.id))}>
        {action.label || 'Open the report'}
      </Button>
    )
  return null
}

// HostLink is a button the app adds itself (Directions, Call, Add to
// calendar): a plain link that doesn't count as the card's action.
export function HostLink({ href, icon, children, download }: { href: string; icon: ReactNode; children: ReactNode; download?: boolean }) {
  const ext = /^https?:/.test(href)
  return (
    <a className={buttonClass('secondary', 'sm')} href={href} download={download || undefined} target={ext ? '_blank' : undefined} rel={ext ? 'noopener noreferrer' : undefined}>
      {icon}
      {children}
    </a>
  )
}
