import type { Entry, SessionState } from '../../api/types'

// runTitle is what the header's centre says Alexa is on: the plan's title,
// a scheduled job's name, or the first line of the request that set it to
// work. Idle, it says nothing.
export function runTitle(session: Pick<SessionState, 'status' | 'plan' | 'control'>, exchange: Entry[]): string {
  if (session.status === 'idle' && !session.control) return ''
  const plan = session.plan?.title?.trim()
  if (plan) return plan
  const first = exchange[0]
  if (!first) return ''
  if (first.data?.scheduled) {
    const name = first.text.replace(/^Scheduled:\s*/, '').trim()
    return name ? `${name} · scheduled` : 'Scheduled'
  }
  const ask = exchange.find((e) => e.kind === 'user')
  return ask ? firstLine(ask.text) : ''
}

// firstLine is the start of a message, short enough for one line.
export function firstLine(text: string, max = 60): string {
  const line = text.trim().split(/\n/)[0].replace(/\s+/g, ' ')
  if (line.length <= max) return line
  const cut = line.slice(0, max - 1)
  const space = cut.lastIndexOf(' ')
  return `${(space > max * 0.6 ? cut.slice(0, space) : cut).replace(/[\s,.;:–-]+$/, '')}…`
}
