import { useEffect, useState } from 'react'
import { useConnection } from '../state/selectors'
import { Pill } from '../ui/Pill'
import { Offline } from './icons'
import './composer.css'

// How long the connection may be down before the header says so: a blip
// while the socket reconnects isn't worth a word, and the first connect
// gets a little longer.
const quietFor = { connecting: 2500, reconnecting: 1200, offline: 0, live: 0 }

// ConnectionPill is the header's quiet "Reconnecting…" or "Offline", so a
// frozen screen can be told apart from a dead connection (IX-F7).
export function ConnectionPill() {
  const c = useConnection()
  const [late, setLate] = useState(false)
  useEffect(() => {
    setLate(false)
    if (c === 'live') return
    const t = setTimeout(() => setLate(true), quietFor[c])
    return () => clearTimeout(t)
  }, [c])
  if (c === 'live' || !late) return null
  return <ConnectionPillView offline={c === 'offline'} />
}

export function ConnectionPillView({ offline }: { offline: boolean }) {
  return (
    <span className="conn-pill" role="status" title={offline ? 'No connection. Alexa keeps working on the server; this screen catches up when you’re back.' : 'Reconnecting to Alexa'}>
      <Pill tone={offline ? 'warn' : 'neutral'}>
        {offline ? <Offline size={12} /> : <span className="spinner" aria-hidden="true" />}
        {offline ? 'Offline' : 'Reconnecting…'}
      </Pill>
    </span>
  )
}
