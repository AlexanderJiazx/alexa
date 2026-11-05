import { Mark } from '../../ui/Mark'
import './caption.css'

// BackgroundChip says Alexa is busy with a scheduled job off screen, so its
// work isn't mistaken for the exchange on screen (IX-F5, R6).
export function BackgroundChip({ label }: { label: string }) {
  return (
    <div className="bg-chip" role="status">
      <Mark state="working" size="sm" />
      <span className="bg-name">{label}</span>
      <span className="bg-sub">in the background</span>
    </div>
  )
}
