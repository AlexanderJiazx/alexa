import { useEffect, useId, useRef, useState } from 'react'
import { Button } from '../../ui/Button'
import { Input } from '../../ui/Field'
import { HandBack as HandBackIcon } from '../../ui/icons'

type Props = {
  // label is "Hand back" while Alexa is waiting on the browser, "Done"
  // when nothing is.
  label: string
  // ask offers a note for Alexa first (IX-F24); without it the browser is
  // handed back at once.
  ask: boolean
  onHandBack: (note: string) => void
}

// HandBack gives the browser back to Alexa, with an optional note ("I’ve
// entered the code") in a small popover under the button.
export function HandBack({ label, ask, onHandBack }: Props) {
  const [open, setOpen] = useState(false)
  const [note, setNote] = useState('')
  const box = useRef<HTMLDivElement>(null)
  const field = useRef<HTMLInputElement>(null)
  const id = useId()

  const close = (refocus: boolean) => {
    setOpen(false)
    if (refocus) box.current?.querySelector('button')?.focus()
  }
  const done = () => {
    setOpen(false)
    onHandBack(note.trim())
    setNote('')
  }

  // A press outside or Escape puts it away; the note stays for next time.
  useEffect(() => {
    if (!open) return
    field.current?.focus({ preventScroll: true })
    const down = (e: PointerEvent) => {
      if (!box.current?.contains(e.target as Node)) close(false)
    }
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        close(true)
      }
    }
    document.addEventListener('pointerdown', down, true)
    document.addEventListener('keydown', key, true)
    return () => {
      document.removeEventListener('pointerdown', down, true)
      document.removeEventListener('keydown', key, true)
    }
  }, [open])

  return (
    <div ref={box} className="bp-handback">
      <Button
        size="sm"
        variant="primary"
        icon={<HandBackIcon size={16} />}
        aria-haspopup={ask ? 'dialog' : undefined}
        aria-expanded={ask ? open : undefined}
        onClick={() => (ask ? setOpen((o) => !o) : onHandBack(''))}
      >
        {label}
      </Button>
      {open && (
        <form
          className="bp-note"
          role="dialog"
          aria-labelledby={`${id}-l`}
          onSubmit={(e) => {
            e.preventDefault()
            done()
          }}
        >
          <label id={`${id}-l`} htmlFor={`${id}-n`} className="bp-note-title">
            Anything Alexa should know?
          </label>
          <Input ref={field} id={`${id}-n`} value={note} maxLength={280} placeholder="Optional, like “I’ve entered the code”" onChange={(e) => setNote(e.target.value)} />
          <div className="bp-note-btns">
            <Button size="sm" variant="ghost" onClick={() => close(true)}>
              Cancel
            </Button>
            <Button size="sm" variant="primary" type="submit">
              Hand back
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}
