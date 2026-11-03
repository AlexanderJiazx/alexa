import { createStore, useSelector } from '../state/store'

// Spoken replies: after the user speaks, what Alexa says is read aloud with
// the browser's speech synthesis. speaking is on while it reads, so the
// composer can show "Speaking…" with a way to stop it (IX-F21).

export const speaking = createStore<{ on: boolean; text: string }>({ on: false, text: '' })

export const useSpeaking = () => useSelector(speaking, (s) => s.on)

// Replies are read one after another: one that comes while another is
// being read waits its turn rather than cutting it off. Only the line being
// read is ever with the synth, so the poll below can tell when it's done.
type Line = { text: string; onEnd: () => void; over: boolean; stop?: () => void }
const queue: Line[] = []
let current: Line | null = null

// speak reads text aloud once what's before it has been read, and calls
// onEnd when it's done or stopped. It returns a stop for this line alone.
export function speak(text: string, onEnd: () => void = () => {}): () => void {
  const synth = typeof window !== 'undefined' ? window.speechSynthesis : undefined
  if (!synth || !text) {
    onEnd()
    return () => {}
  }
  const line: Line = { text, onEnd, over: false }
  queue.push(line)
  if (!current) {
    // Nothing of ours is being read: clear whatever the synth was left with.
    synth.cancel()
    next(synth)
  }
  return () => drop(line)
}

function next(synth: SpeechSynthesis) {
  const line = queue.shift() ?? null
  current = line
  if (!line) {
    speaking.set({ on: false, text: '' })
    return
  }
  const u = new SpeechSynthesisUtterance(line.text)
  u.rate = 1.03
  const voices = synth.getVoices()
  const pick = voices.find((v) => /Samantha|Google US English|Natural|Aria|Jenny/i.test(v.name) && v.lang.startsWith('en')) || voices.find((v) => v.lang.startsWith((navigator.language || 'en').slice(0, 2)))
  if (pick) u.voice = pick
  // Some engines never fire end (Chrome drops it on long texts), so a poll
  // notices the synth went quiet.
  const poll = setInterval(() => {
    if (!synth.speaking && !synth.pending) done()
  }, 750)
  const done = () => {
    if (line.over) return
    line.over = true
    clearInterval(poll)
    line.onEnd()
    if (current === line) next(synth)
  }
  line.stop = () => {
    if (line.over) return
    synth.cancel()
    done()
  }
  u.onend = done
  u.onerror = done
  speaking.set({ on: true, text: line.text })
  synth.speak(u)
}

// drop stops one line: the one being read goes on to the next; one still
// waiting is taken out of the queue.
function drop(line: Line) {
  if (line.over) return
  if (line === current) return line.stop?.()
  const i = queue.indexOf(line)
  if (i >= 0) queue.splice(i, 1)
  line.over = true
  line.onEnd()
}

// stopSpeaking stops what's being read aloud, and drops what waits.
export function stopSpeaking() {
  for (const line of queue.splice(0)) {
    line.over = true
    line.onEnd()
  }
  current?.stop?.()
}
