import type { BlockOf } from '../../../api/blocks'
import { Check, Info, Warning } from '../../../ui/icons'
import { Bulb } from '../icons'
import './callout.css'

const glyphs = { info: Info, tip: Bulb, warning: Warning, success: Check }

// Callout is one short note set apart: a tip, a warning, a success, or
// something to know. The tone shows as a glyph and a quiet wash; the words
// carry the meaning.
export function Callout({ block: b }: { block: BlockOf<'callout'> }) {
  const G = glyphs[b.tone] ?? Info
  return (
    <div className={`b-callout t-${b.tone in glyphs ? b.tone : 'info'}`} role="note">
      <G size={18} />
      <div className="b-callout-t">
        {b.title && <b>{b.title}</b>}
        <p>{b.text}</p>
      </div>
    </div>
  )
}
