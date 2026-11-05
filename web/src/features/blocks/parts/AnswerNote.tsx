import { clock } from '../../../lib/format'

type Props = {
  // chose is the label of the choice the answer was, when it was one.
  chose?: string
  // said is the answer in the user's own words, when it wasn't.
  said?: string
  at?: number
}

// AnswerNote is the one way a past answer is written where the answer
// itself isn't on screen as a message: "You chose Pick tacos · 11:22 AM",
// or "You answered “Neither” · 11:22 AM". Reflect's questions and an
// expanded entry's cards both use it.
export function AnswerNote({ chose, said, at }: Props) {
  if (!chose && !said) return null
  return (
    <p className="b-answered num">
      {chose ? (
        <>
          You chose <b>{chose}</b>
        </>
      ) : (
        <>
          You answered <q>{said}</q>
        </>
      )}
      {at ? ` · ${clock(at)}` : ''}
    </p>
  )
}
