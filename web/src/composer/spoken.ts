import type { ServerEvent } from '../api/realtime'
import { speak } from './speech'

// Alexa's replies are read aloud after the user speaks to it, and only
// those: what it says or asks in the run that answers a voice message. A
// typed message, the end of that run or a scheduled job starting turns it
// off again, so one voice message never leaves every later reply read
// aloud, a scheduled job's hours later included.
//
// on is whether replies are read; after is the last entry already read, or
// there before the message; ran is whether Alexa has been at work since the
// message went, so an idle state from before its run doesn't end it.
type Voiced = { on: boolean; after: number; ran: boolean }
export const voiced: Voiced = { on: false, after: 0, ran: false }

// voiceSent is the outbox telling of a message as it goes: by voice or
// typed, the last entry in the feed, and whether Alexa is at work (a
// message sent then is read by the run under way).
export function voiceSent(voice: boolean, after: number, working: boolean) {
  voiced.on = voice
  voiced.after = after
  voiced.ran = working
}

// voiceFailed: a message that couldn't go has no reply to read.
export function voiceFailed() {
  voiced.on = false
}

// speakReplies follows the session's events and reads the replies.
export function speakReplies(ev: ServerEvent) {
  if (ev.type === 'reset') voiced.on = false
  if (ev.type === 'state' && ev.data) {
    if (ev.data.status === 'working') voiced.ran = true
    // The run that answered is over, and no message waits for another.
    else if (ev.data.status === 'idle' && voiced.ran && !ev.data.queued) voiced.on = false
    return
  }
  if (ev.type !== 'entry' || !ev.data || !voiced.on) return
  const en = ev.data
  if (en.id <= voiced.after) return
  // A scheduled job is a run of its own, not a reply.
  if (en.data?.scheduled) {
    voiced.on = false
    return
  }
  if (en.kind === 'say' || en.kind === 'ask') {
    voiced.after = en.id
    speak(en.text)
  }
}
