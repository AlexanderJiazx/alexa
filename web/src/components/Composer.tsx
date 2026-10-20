import { useEffect, useRef, useState } from 'react'
import type { Attachment } from '../lib/api'
import { api } from '../lib/api'
import { VoiceSession } from '../lib/voice'
import { Bars, Close, File, Keyboard, Plus, Up } from './Icons'

type Props = {
  placeholder: string
  disabled?: boolean
  // voice is true while the bar is in voice mode (listening).
  voice: boolean
  onVoice: (on: boolean) => void
  onSend: (text: string, attachments: Attachment[], viaVoice: boolean) => void
  onListen: (committed: string, partial: string) => void
  onError: (msg: string) => void
}

// Composer is the bottom input bar. It morphs between a text field and a
// round voice button, like the design: tap the bars to talk, tap the disc to
// send what you said, or switch back to the keyboard.
export function Composer({ placeholder, disabled, voice, onVoice, onSend, onListen, onError }: Props) {
  const [text, setText] = useState('')
  const [files, setFiles] = useState<Attachment[]>([])
  const [uploading, setUploading] = useState(0)
  const [level, setLevel] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const session = useRef<VoiceSession | null>(null)
  const filesRef = useRef(files)
  filesRef.current = files
  const sendRef = useRef(onSend)
  sendRef.current = onSend

  useEffect(() => {
    if (!voice) {
      session.current?.cancel()
      session.current = null
      return
    }
    const s = new VoiceSession({
      onText: (c, p) => onListen(c, p),
      onTurn: (t) => {
        // Endpointing: the user finished speaking — send it.
        if (session.current !== s) return
        session.current = null
        s.cancel()
        if (t.trim()) sendRef.current(t.trim(), filesRef.current, true)
        setFiles([])
        onVoice(false)
      },
      onLevel: (l) => setLevel(l),
      onError: (m) => {
        onError(m)
        onVoice(false)
      },
    })
    session.current = s
    onListen('', '')
    s.start().catch(() => onVoice(false))
    return () => {
      s.cancel()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice])

  useEffect(() => {
    if (!voice && !disabled) inputRef.current?.focus()
  }, [voice, disabled])

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault()
    const t = text.trim()
    if ((!t && files.length === 0) || uploading > 0) return
    onSend(t, files, false)
    setText('')
    setFiles([])
  }

  const finishVoice = async () => {
    const s = session.current
    session.current = null
    if (!s) return onVoice(false)
    const t = await s.finish()
    onVoice(false)
    if (t.trim()) onSend(t.trim(), files, true)
    setFiles([])
  }

  const addFiles = async (list: FileList | null) => {
    if (!list) return
    for (const f of Array.from(list)) {
      setUploading((n) => n + 1)
      try {
        const a = await api.upload(f)
        setFiles((fs) => [...fs, a])
      } catch (err) {
        onError(`Couldn’t upload ${f.name}: ${(err as Error).message}`)
      } finally {
        setUploading((n) => n - 1)
      }
    }
    if (fileRef.current) fileRef.current.value = ''
  }

  const bars = [0.55, 0.8, 1, 0.8, 0.55]
  const amp = Math.min(1, level * 9)

  return (
    <>
      <div
        className={`bar ${voice ? 'voice' : 'text'}`}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault()
          addFiles(e.dataTransfer.files)
        }}
      >
        {voice ? (
          <button type="button" className="disc" aria-label="Done talking — send" onClick={finishVoice}>
            {bars.map((b, i) => (
              <span key={i} style={{ height: 8 + 22 * b * (0.25 + amp * 0.75), transition: 'height 90ms linear' }} />
            ))}
          </button>
        ) : (
          <form onSubmit={submit}>
            <label className="fb" aria-label="Attach files">
              <Plus size={20} />
              <input ref={fileRef} type="file" multiple className="sr" onChange={(e) => addFiles(e.target.files)} />
            </label>
            {files.map((f) => (
              <button key={f.path} type="button" className="chip" title={`Remove ${f.name}`} onClick={() => setFiles(files.filter((x) => x !== f))}>
                <File size={15} />
                <span>{f.name}</span>
                <Close size={13} />
              </button>
            ))}
            {uploading > 0 && <span className="spinner" aria-label="Uploading" />}
            <label htmlFor="composer" className="sr">
              Message
            </label>
            <input
              id="composer"
              ref={inputRef}
              type="text"
              autoComplete="off"
              placeholder={placeholder}
              value={text}
              disabled={disabled}
              onChange={(e) => setText(e.target.value)}
              onPaste={(e) => {
                if (e.clipboardData.files.length) {
                  e.preventDefault()
                  addFiles(e.clipboardData.files)
                }
              }}
            />
            {text.trim() || files.length ? (
              <button type="submit" className="send" aria-label="Send" disabled={uploading > 0}>
                <Up size={18} />
              </button>
            ) : (
              <button type="button" className="icon-btn" aria-label="Talk" onClick={() => onVoice(true)} disabled={disabled}>
                <Bars size={22} />
              </button>
            )}
          </form>
        )}
      </div>
      {voice && (
        <div className="voice-side">
          <button type="button" className="round" aria-label="Use keyboard" onClick={() => onVoice(false)}>
            <Keyboard size={18} />
          </button>
          <button
            type="button"
            className="round"
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
