import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react'
import { api } from '../api/client'
import { setVoice as onVoice, toast } from '../state/actions'
import { fromIme } from '../lib/ime'
import { useMedia } from '../lib/useMedia'
import { setHeard as onListen } from '../state/live'
import { useSession, useUI } from '../state/selectors'
import { Bars, Close, Keyboard, Plus, Up } from '../ui/icons'
import { AttachmentTray, type Attached, type Upload } from './AttachmentTray'
import './composer.css'
import { focusComposer, registerComposer, registerPrefill } from './focus'
import { HintRow } from './HintRow'
import { submit, watch, type Item, type Outgoing } from './outbox'
import { Receipts } from './Receipts'
import { stopSpeaking } from './speech'
import { VoiceAborted, VoiceSession } from './voice'

// placeholderFor is what the empty composer invites, given what Alexa is
// doing. It's the field's only visible label, so on a phone it's worded to
// fit on one line whole.
function placeholderFor(kind: string | undefined, working: boolean, short = false) {
  if (kind === 'question') return 'Type your answer'
  if (kind === 'takeover') return short ? 'Reply, or take over' : 'Tell Alexa something, or take over'
  if (kind === 'login') return short ? 'Or reply instead' : 'Or tell Alexa something instead'
  if (working) return short ? 'Add or change course' : 'Add something or change course'
  return 'Ask Alexa to do anything'
}

// The key hints under the composer show for the first few messages, then
// get out of the way.
const hintKey = 'alexa.composerHints'
const hintSends = 3
function hintsLeft() {
  try {
    return Number(localStorage.getItem(hintKey) || 0) < hintSends
  } catch {
    return true
  }
}
function usedHint() {
  try {
    const n = Number(localStorage.getItem(hintKey) || 0) + 1
    localStorage.setItem(hintKey, String(n))
    return n < hintSends
  } catch {
    return false
  }
}

// The text grows from one line to eight, then scrolls.
const lineH = 22
const maxLines = 8

// Composer is the bottom input bar. It morphs between a text field and a
// round voice button: tap the bars to talk, tap the disc to send what you
// said, or switch back to the keyboard. Sending is optimistic: the field
// clears at once, and a message that couldn't go comes back with Retry.
export function Composer() {
  // voice is true while the bar is in voice mode (listening).
  const voice = useUI((u) => u.voice)
  const phone = useMedia('(max-width: 640px)')
  const { placeholder, working } = useSession((s) => ({ placeholder: placeholderFor(s.status === 'needs' ? s.needs?.kind : undefined, s.status === 'working', phone), working: s.status === 'working' }))
  const [text, setText] = useState('')
  const [files, setFiles] = useState<Attached[]>([])
  const [uploads, setUploads] = useState<Upload[]>([])
  const [level, setLevel] = useState(0)
  const [focused, setFocused] = useState(false)
  const [hints, setHints] = useState(hintsLeft)
  const bar = useRef<HTMLDivElement>(null)
  const form = useRef<HTMLFormElement>(null)
  const field = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const session = useRef<VoiceSession | null>(null)
  const textRef = useRef(text)
  textRef.current = text
  const filesRef = useRef(files)
  filesRef.current = files

  // The receipts follow the session from here.
  useEffect(() => watch(), [])

  useEffect(() => {
    registerComposer(field.current)
    return () => registerComposer(null)
  }, [voice])

  // Text put in from elsewhere (the home board's chips) replaces what's
  // here; the caret goes to its end once the field shows it.
  const caretToEnd = useRef(false)
  useEffect(() => {
    registerPrefill((t) => {
      if (voice) onVoice(false)
      caretToEnd.current = true
      setText(t)
    })
    return () => registerPrefill(null)
  }, [voice])
  useEffect(() => {
    if (!caretToEnd.current || !field.current) return
    caretToEnd.current = false
    focusComposer()
  })

  // send hands a message to the outbox and clears the field at once. If it
  // couldn't go, the text comes back, unless something new was typed
  // meanwhile; the receipt offers Retry either way.
  const send = async (msg: Outgoing) => {
    stopSpeaking()
    setText('')
    setFiles([])
    setHints(usedHint())
    const it = await submit(msg)
    if (it.state === 'failed' && textRef.current === '' && filesRef.current.length === 0) {
      setText(msg.text)
      setFiles(msg.attachments ?? [])
    }
  }

  // A retried message that went clears the field it was put back in.
  const onRetried = (it: Item) => {
    if (it.state !== 'failed' && textRef.current === it.msg.text) {
      setText('')
      setFiles([])
    }
  }

  useEffect(() => {
    if (!voice) {
      session.current?.cancel()
      session.current = null
      return
    }
    stopSpeaking()
    const s = new VoiceSession({
      onText: (c, p) => onListen(c, p),
      onTurn: (t) => {
        // Endpointing: the user finished speaking, so it's sent.
        if (session.current !== s) return
        session.current = null
        s.cancel()
        if (t.trim()) send({ text: t.trim(), attachments: filesRef.current, voice: true })
        onVoice(false)
      },
      onLevel: (l) => setLevel(l),
      onError: (m) => {
        toast('Voice', m)
        onVoice(false)
      },
    })
    session.current = s
    onListen('', '')
    s.start().catch((e) => {
      // A cancelled start is already over; only a live one turns voice off.
      if (!(e instanceof VoiceAborted) && session.current === s) onVoice(false)
    })
    return () => {
      s.cancel()
      if (session.current === s) session.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice])

  // The field takes focus when the app opens, and when the user leaves
  // voice. Not when it opens on a touch screen: nobody is typing yet, and
  // it would raise the keyboard (and start a screen reader past the page).
  const mounted = useRef(false)
  useEffect(() => {
    const first = !mounted.current
    mounted.current = true
    if (voice) return
    if (first && matchMedia('(pointer: coarse)').matches) return
    field.current?.focus({ preventScroll: true })
  }, [voice])

  // Grow with the text, one line to eight.
  useLayoutEffect(() => {
    const ta = field.current
    if (!ta) return
    ta.style.height = 'auto'
    const cs = getComputedStyle(ta)
    const max = lineH * maxLines + parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom)
    ta.style.height = `${Math.min(ta.scrollHeight, max)}px`
    ta.classList.toggle('scrolls', ta.scrollHeight > max)
  }, [text, voice])

  // The stage, the dock and the toasts make room as the composer grows:
  // they all read --composer-h from the app.
  useLayoutEffect(() => {
    const f = form.current
    const host = (bar.current?.closest('.app') as HTMLElement | null) ?? bar.current?.parentElement
    if (!f || !host || voice) return
    const set = () => {
      const h = Math.max(56, Math.round(f.getBoundingClientRect().height) + 2)
      if (h === 56) host.style.removeProperty('--composer-h')
      else host.style.setProperty('--composer-h', `${h}px`)
    }
    set()
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(set)
    ro?.observe(f)
    return () => {
      ro?.disconnect()
      host.style.removeProperty('--composer-h')
    }
  }, [voice])

  const ready = (text.trim() !== '' || files.length > 0) && uploads.every((u) => u.error)

  const submitText = () => {
    if (!ready) return
    send({ text: text.trim(), attachments: files })
  }

  // interrupt stops the step Alexa is on and gives her this instead.
  const interrupt = () => {
    const t = text.trim()
    if (!t || !working) return
    send({ text: t, interrupt: true })
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // The Enter that picks an IME candidate isn't a send.
    if (e.key !== 'Enter' || fromIme(e.nativeEvent)) return
    if (e.shiftKey) return // a new line
    e.preventDefault()
    if ((e.metaKey || e.ctrlKey) && working && files.length === 0) interrupt()
    else submitText()
  }

  const finishVoice = async () => {
    const s = session.current
    session.current = null
    if (!s) return onVoice(false)
    const t = await s.finish()
    onVoice(false)
    if (t.trim()) send({ text: t.trim(), attachments: filesRef.current, voice: true })
  }

  const addFiles = async (list: FileList | null) => {
    if (!list) return
    for (const f of Array.from(list)) {
      const key = `${Date.now()}:${Math.random()}`
      const preview = f.type.startsWith('image/') ? URL.createObjectURL(f) : undefined
      setUploads((u) => [...u, { key, name: f.name, preview }])
      try {
        const a = await api.upload(f)
        setFiles((fs) => [...fs, a])
        setUploads((u) => u.filter((x) => x.key !== key))
      } catch (err) {
        setUploads((u) => u.map((x) => (x.key === key ? { ...x, error: (err as Error).message || 'Couldn’t upload' } : x)))
      } finally {
        if (preview) URL.revokeObjectURL(preview)
      }
    }
    if (fileRef.current) fileRef.current.value = ''
  }

  const bars = [0.55, 0.8, 1, 0.8, 0.55]
  const amp = Math.min(1, level * 9)

  return (
    <>
      <div
        ref={bar}
        className={`bar ${voice ? 'voice' : 'text'}`}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault()
          addFiles(e.dataTransfer.files)
        }}
      >
        {voice ? (
          <button type="button" className="disc" aria-label="Done talking, send" onClick={finishVoice}>
            {bars.map((b, i) => (
              <span key={i} style={{ height: 8 + 22 * b * (0.25 + amp * 0.75) }} />
            ))}
          </button>
        ) : (
          <form
            ref={form}
            className="cmp"
            onSubmit={(e) => {
              e.preventDefault()
              submitText()
            }}
          >
            <Receipts working={working} typing={text.trim() !== '' && files.length === 0} onInterrupt={interrupt} onRetried={onRetried} />
            <AttachmentTray files={files} uploads={uploads} onRemove={(f) => setFiles((fs) => fs.filter((x) => x !== f))} onDismiss={(u) => setUploads((us) => us.filter((x) => x !== u))} />
            <div className="cmp-row">
              <label className="fb" aria-label="Attach files" title="Attach files">
                <Plus size={20} />
                <input ref={fileRef} type="file" multiple className="sr" onChange={(e) => addFiles(e.target.files)} />
              </label>
              <label htmlFor="composer" className="sr">
                Message
              </label>
              <textarea
                id="composer"
                ref={field}
                rows={1}
                autoComplete="off"
                enterKeyHint="send"
                placeholder={placeholder}
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={onKeyDown}
                onFocus={() => setFocused(true)}
                onBlur={() => setFocused(false)}
                onPaste={(e) => {
                  if (e.clipboardData.files.length) {
                    e.preventDefault()
                    addFiles(e.clipboardData.files)
                  }
                }}
              />
              <button type="button" className="icon-btn talk" aria-label="Talk" title="Talk" onClick={() => onVoice(true)}>
                <Bars size={22} />
              </button>
              <button type="submit" className="send" aria-label="Send" title="Send" disabled={!ready}>
                <Up size={18} />
              </button>
            </div>
            <HintRow show={focused && hints && text.trim() !== ''} working={working} />
          </form>
        )}
      </div>
      {voice && (
        <div className="voice-side">
          <button type="button" className="voice-round" aria-label="Use keyboard" onClick={() => onVoice(false)}>
            <Keyboard size={18} />
          </button>
          <button
            type="button"
            className="voice-round"
            aria-label="Cancel"
            onClick={() => {
              session.current?.cancel()
              session.current = null
              onVoice(false)
            }}
          >
            <Close size={18} />
          </button>
        </div>
      )}
    </>
  )
}
