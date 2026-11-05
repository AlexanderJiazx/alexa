import { useCallback, useState } from 'react'
import { retry, submit, type Outgoing } from '../../composer/outbox'

// useReply sends a card's answer as a visible message, once: while it's on
// its way, and after it went, the card's buttons are off, so a double tap
// can't answer twice (FE-10). If it couldn't be sent, the card says so and
// offers Retry, which goes again under the same clientId.
export type Reply = {
  busy: boolean
  sent: boolean
  // which is what was picked, for the card to mark it.
  which: string
  error: string
  send: (msg: Outgoing, which?: string) => Promise<boolean>
  retry: () => void
}

export function useReply(onSent?: () => void): Reply {
  const [s, set] = useState({ busy: false, sent: false, which: '', error: '', id: '' })
  const settle = useCallback(
    (ok: boolean, id: string, which: string, error = '') => {
      set({ busy: false, sent: ok, which, error: ok ? '' : error || 'Couldn’t send', id: ok ? '' : id })
      if (ok) onSent?.()
      return ok
    },
    [onSent],
  )
  const send = useCallback(
    async (msg: Outgoing, which = msg.text) => {
      if (s.busy || s.sent) return false
      set({ busy: true, sent: false, which, error: '', id: '' })
      const it = await submit(msg, 'ask')
      return settle(it.state !== 'failed', it.clientId, which, it.error)
    },
    [s.busy, s.sent, settle],
  )
  const again = useCallback(() => {
    if (!s.id || s.busy) return
    const which = s.which
    const id = s.id
    set({ busy: true, sent: false, which, error: '', id })
    retry(id).then((it) => settle(!!it && it.state !== 'failed', id, which, it?.error))
  }, [s.id, s.busy, s.which, settle])
  return { busy: s.busy, sent: s.sent, which: s.which, error: s.error, send, retry: again }
}
