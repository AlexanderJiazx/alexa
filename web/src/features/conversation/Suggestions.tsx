import { useState, type CSSProperties } from 'react'
import { Chip } from '../../ui/Chip'

// onPick sends the chip's words. It may resolve whether they went; when
// they didn't, the chips come back, so the suggestion can be tapped again.
type Props = { suggestions: string[]; onPick: (text: string) => Promise<boolean> | void }

// ifUnsent calls back once a send has failed: it resolved false, or threw.
export function ifUnsent(sent: Promise<boolean> | void, back: () => void) {
  if (sent instanceof Promise)
    sent.then(
      (ok) => {
        if (!ok) back()
      },
      back,
    )
}

// Suggestions shows the follow-ups Alexa offers under its final say (WP-08):
// up to three chips. Tapping one sends it as the user's own, visible
// message; the chips step aside at once so it can't go twice, and come
// back if it couldn't be sent.
export function Suggestions({ suggestions, onPick }: Props) {
  const [picked, setPicked] = useState(false)
  if (picked || suggestions.length === 0) return null
  const pick = (s: string) => {
    setPicked(true)
    ifUnsent(onPick(s), () => setPicked(false))
  }
  return (
    <div className="follow-ups" role="group" aria-label="Suggested replies">
      {suggestions.slice(0, 3).map((s, i) => (
        <Chip key={s} className="follow-up" style={{ '--i': i } as CSSProperties} onClick={() => pick(s)}>
          {s}
        </Chip>
      ))}
    </div>
  )
}
