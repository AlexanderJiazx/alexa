import type { Entry } from '../../api/types'
import { Button } from '../../ui/Button'
import { Play, Stop } from '../../ui/icons'
import { useIsLatest } from './FailureCard'
import { useReply } from './reply'
import './ask.css'

export const resumeText = 'Carry on where you left off.'

// StoppedCard is the quiet line a stopped run leaves, with Resume while
// nothing has happened since (IX-F12). Resume is a visible message, so the
// conversation shows it was picked up again. NoteEntry draws it for the
// "Stopped" note.
export function StoppedCard({ entry: e, live }: { entry: Entry; live: boolean }) {
  const latest = useIsLatest(e.id)
  const reply = useReply()
  const can = live && latest && !reply.sent
  return (
    <div className="ask-stopped" role="status">
      <Stop size={14} />
      <span>{e.text || 'Stopped'}</span>
      {can && (
        <Button size="sm" variant="ghost" icon={<Play size={14} />} loading={reply.busy} onClick={() => reply.send({ text: resumeText })}>
          Resume
        </Button>
      )}
      {reply.error && (
        <button type="button" className="linkish" onClick={reply.retry}>
          Couldn’t send · Retry
        </button>
      )}
    </div>
  )
}
