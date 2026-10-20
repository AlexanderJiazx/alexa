import { api, wsURL } from './api'

// Live speech-to-text. Primary: Meta realtime ASR proxied by our server
// (24 kHz PCM over WebSocket). Fallbacks: the browser's Web Speech API, then
// recording a WAV and sending it to the batch transcription endpoint.

const workletSrc = `
class PCM24 extends AudioWorkletProcessor {
  constructor() { super(); this.ratio = sampleRate / 24000; this.t = 0; this.buf = new Int16Array(1920); this.n = 0; this.sum = 0; }
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (!ch) return true;
    for (let i = 0; i < ch.length; i++) {
      this.t += 1;
      if (this.t < this.ratio) continue;
      this.t -= this.ratio;
      const s = Math.max(-1, Math.min(1, ch[i]));
      this.sum += s * s;
      this.buf[this.n++] = s < 0 ? s * 0x8000 : s * 0x7fff;
      if (this.n === this.buf.length) {
        this.port.postMessage({ pcm: this.buf.buffer, level: Math.sqrt(this.sum / this.n) }, [this.buf.buffer]);
        this.buf = new Int16Array(1920); this.n = 0; this.sum = 0;
      }
    }
    return true;
  }
}
registerProcessor('pcm24', PCM24);
`

export type VoiceHandlers = {
  onText: (committed: string, partial: string) => void
  onTurn: (text: string) => void // the user finished a sentence (endpointing)
  onLevel?: (level: number) => void
  onError: (msg: string) => void
}

type SR = {
  continuous: boolean
  interimResults: boolean
  lang: string
  start: () => void
  stop: () => void
  abort: () => void
  onresult: ((e: { resultIndex: number; results: ArrayLike<{ isFinal: boolean; 0: { transcript: string } }> }) => void) | null
  onerror: ((e: { error: string }) => void) | null
  onend: (() => void) | null
}

export class VoiceSession {
  private h: VoiceHandlers
  private ctx?: AudioContext
  private stream?: MediaStream
  private node?: AudioWorkletNode
  private ws?: WebSocket
  private sr?: SR
  private mode: 'meta' | 'webspeech' | 'record' = 'meta'
  private committed = ''
  private partial = ''
  private recorded: Int16Array[] = []
  private finalWaiters: ((t: string) => void)[] = []
  private closed = false

  constructor(h: VoiceHandlers) {
    this.h = h
  }

  get text() {
    return [this.committed, this.partial].filter(Boolean).join(' ').trim()
  }

  async start() {
    try {
      this.stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, channelCount: 1 } })
    } catch {
      this.h.onError('Microphone access was denied')
      throw new Error('mic denied')
    }
    try {
      await this.startMeta()
      return
    } catch (e) {
      console.warn('realtime ASR unavailable, falling back', e)
    }
    if (this.startWebSpeech()) return
    this.mode = 'record'
    await this.startAudio()
  }

  private async startAudio() {
    try {
      this.ctx = new AudioContext({ sampleRate: 24000 })
    } catch {
      this.ctx = new AudioContext()
    }
    const url = URL.createObjectURL(new Blob([workletSrc], { type: 'application/javascript' }))
    await this.ctx.audioWorklet.addModule(url)
    URL.revokeObjectURL(url)
    const src = this.ctx.createMediaStreamSource(this.stream!)
    this.node = new AudioWorkletNode(this.ctx, 'pcm24')
    this.node.port.onmessage = (e: MessageEvent<{ pcm: ArrayBuffer; level: number }>) => {
      this.h.onLevel?.(e.data.level)
      if (this.mode === 'meta' && this.ws?.readyState === WebSocket.OPEN) this.ws.send(e.data.pcm)
      else if (this.mode === 'record') this.recorded.push(new Int16Array(e.data.pcm))
    }
    src.connect(this.node)
    // Keep the graph pulling without audible output.
    const mute = this.ctx.createGain()
    mute.gain.value = 0
    this.node.connect(mute).connect(this.ctx.destination)
  }

  private startMeta(): Promise<void> {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(wsURL('/api/voice'))
      ws.binaryType = 'arraybuffer'
      this.ws = ws
      let ready = false
      const timer = setTimeout(() => {
        if (!ready) {
          ws.close()
          reject(new Error('timeout'))
        }
      }, 8000)
      ws.onopen = () => ws.send(JSON.stringify({ type: 'start', mode: 'ENDPOINTING' }))
      ws.onerror = () => {
        if (!ready) {
          clearTimeout(timer)
          reject(new Error('ws error'))
        }
      }
      ws.onclose = () => {
        if (!ready) {
          clearTimeout(timer)
          reject(new Error('closed'))
        } else this.flushWaiters()
      }
      ws.onmessage = async (ev) => {
        if (typeof ev.data !== 'string') return
        let m: { type?: string; transcript?: string; final?: boolean; message?: string }
        try {
          m = JSON.parse(ev.data)
        } catch {
          return
        }
        switch (m.type) {
          case 'ready':
            ready = true
            clearTimeout(timer)
            try {
              await this.startAudio()
              resolve()
            } catch (e) {
              reject(e)
            }
            break
          case 'transcript':
            this.partial = (m.transcript || '').trim()
            if (m.final) {
              this.commit(this.partial)
            }
            this.emit()
            break
          case 'speechComplete':
            this.commit((m.transcript || this.partial).trim())
            this.emit()
            if (this.committed) this.h.onTurn(this.committed)
            break
          case 'error':
            if (!ready) {
              clearTimeout(timer)
              reject(new Error(m.message))
            } else this.h.onError(m.message || 'Speech recognition error')
            break
          case 'closed':
            this.flushWaiters()
            break
        }
      }
    })
  }

  private commit(t: string) {
    if (t) this.committed = [this.committed, t].filter(Boolean).join(' ').trim()
    this.partial = ''
  }

  private emit() {
    this.h.onText(this.committed, this.partial)
  }

  private flushWaiters() {
    const t = this.text
    this.finalWaiters.splice(0).forEach((f) => f(t))
  }

  private startWebSpeech(): boolean {
    const w = window as unknown as { SpeechRecognition?: new () => SR; webkitSpeechRecognition?: new () => SR }
    const Ctor = w.SpeechRecognition || w.webkitSpeechRecognition
    if (!Ctor) return false
    this.mode = 'webspeech'
    const sr = new Ctor()
    sr.continuous = true
    sr.interimResults = true
    sr.lang = navigator.language || 'en-US'
    sr.onresult = (e) => {
      let interim = ''
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const r = e.results[i]
        if (r.isFinal) this.commit(r[0].transcript.trim())
        else interim += r[0].transcript
      }
      this.partial = interim.trim()
      this.emit()
    }
    sr.onerror = (e) => {
      if (e.error !== 'no-speech' && e.error !== 'aborted') this.h.onError('Speech recognition error: ' + e.error)
    }
    sr.onend = () => {
      if (!this.closed) {
        try {
          sr.start()
        } catch {
          /* ignore */
        }
      } else this.flushWaiters()
    }
    sr.start()
    this.sr = sr
    return true
  }

  /** Stop listening and resolve with the final transcript. */
  async finish(): Promise<string> {
    if (this.closed) return this.text
    this.closed = true
    this.node?.disconnect()
    this.stream?.getTracks().forEach((t) => t.stop())
    let text = this.text
    if (this.mode === 'meta' && this.ws && this.ws.readyState === WebSocket.OPEN) {
      const done = new Promise<string>((res) => this.finalWaiters.push(res))
      this.ws.send(JSON.stringify({ type: 'end' }))
      text = await Promise.race([done, new Promise<string>((res) => setTimeout(() => res(this.text), 3500))])
      this.ws.close()
    } else if (this.mode === 'webspeech' && this.sr) {
      const done = new Promise<string>((res) => this.finalWaiters.push(res))
      this.sr.stop()
      text = await Promise.race([done, new Promise<string>((res) => setTimeout(() => res(this.text), 1500))])
    } else if (this.mode === 'record' && this.recorded.length) {
      try {
        text = await api.transcribe(toWav(this.recorded, 24000))
      } catch {
        this.h.onError('Could not transcribe audio')
      }
    }
    void this.ctx?.close()
    return text.trim()
  }

  cancel() {
    this.closed = true
    this.node?.disconnect()
    this.stream?.getTracks().forEach((t) => t.stop())
    this.ws?.close()
    this.sr?.abort()
    void this.ctx?.close()
  }
}

function toWav(chunks: Int16Array[], rate: number): Blob {
  const len = chunks.reduce((n, c) => n + c.length, 0)
  const buf = new ArrayBuffer(44 + len * 2)
  const v = new DataView(buf)
  const w = (o: number, s: string) => [...s].forEach((ch, i) => v.setUint8(o + i, ch.charCodeAt(0)))
  w(0, 'RIFF')
  v.setUint32(4, 36 + len * 2, true)
  w(8, 'WAVE')
  w(12, 'fmt ')
  v.setUint32(16, 16, true)
  v.setUint16(20, 1, true)
  v.setUint16(22, 1, true)
  v.setUint32(24, rate, true)
  v.setUint32(28, rate * 2, true)
  v.setUint16(32, 2, true)
  v.setUint16(34, 16, true)
  w(36, 'data')
  v.setUint32(40, len * 2, true)
  let o = 44
  for (const c of chunks) for (let i = 0; i < c.length; i++, o += 2) v.setInt16(o, c[i], true)
  return new Blob([buf], { type: 'audio/wav' })
}

// ---------- spoken replies (local speech synthesis) ----------

export function speak(text: string, onEnd: () => void): () => void {
  const synth = window.speechSynthesis
  if (!synth || !text) {
    onEnd()
    return () => {}
  }
  synth.cancel()
  const u = new SpeechSynthesisUtterance(text)
  u.rate = 1.03
  const voices = synth.getVoices()
  const pick = voices.find((v) => /Samantha|Google US English|Natural|Aria|Jenny/i.test(v.name) && v.lang.startsWith('en')) || voices.find((v) => v.lang.startsWith((navigator.language || 'en').slice(0, 2)))
  if (pick) u.voice = pick
  u.onend = onEnd
  u.onerror = onEnd
  synth.speak(u)
  return () => {
    synth.cancel()
    onEnd()
  }
}
