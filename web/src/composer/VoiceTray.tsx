import { useHeard } from '../state/live'
import { useWorking } from '../state/selectors'

// VoiceTray shows what the microphone heard while the user talks. It reads
// the transcript's own store, so a new word re-renders only this.
export function VoiceTray() {
  const heard = useHeard()
  const working = useWorking()
  return (
    <div className="voice-tray" aria-live="polite">
      <p className={`heard${heard.committed || heard.partial ? '' : ' quiet'}`}>
        <span>
          {heard.committed || heard.partial ? (
            <>
              {heard.committed} <span className="saying">{heard.partial}</span>
            </>
          ) : (
            'Listening…'
          )}
        </span>
      </p>
      <p className="hint">{working ? 'Add something or change course. Tap the circle to send.' : 'Say what you need. Tap the circle when you’re done.'}</p>
    </div>
  )
}
