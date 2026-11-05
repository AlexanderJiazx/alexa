import { StoppedCard } from '../../ask/StoppedCard'
import { Clock } from '../../../ui/icons'
import type { EntryProps } from '../registry'

// NoteEntry is a quiet line from the system: "Stopped", "Scheduled: …". A
// stop gets Resume while nothing has happened since (IX-F12).
export function NoteEntry({ entry: e, mode }: EntryProps) {
  if (e.text === 'Stopped') return <StoppedCard entry={e} live={mode === 'now'} />
  return (
    <div className="note">
      {e.data?.scheduled && <Clock size={13} />}
      <span>{e.text}</span>
    </div>
  )
}
