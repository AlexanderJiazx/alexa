import { useState } from 'react'
import { FollowChip } from '../../features/caption/FollowChip'
import { shallow, useSelector } from '../../state/store'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { Favicon } from '../../ui/Favicon'
import { IconButton } from '../../ui/IconButton'
import { Back, Close, Fwd, Keyboard, Moon, Pointer, Reload, Warning } from '../../ui/icons'
import { Pill, type Tone } from '../../ui/Pill'
import { addressOf, hostParts } from './address'
import type { Live } from './controller'
import { HandBack } from './HandBack'
import { liveStatus, statusLabel, type LiveStatus } from './status'

const coarse = matchMedia('(pointer: coarse)').matches

type Props = {
  live: Live
  url: string
  control: boolean
  // waiting: Alexa is waiting on the browser (a run, or a take-over it
  // asked for), so handing back carries on its work.
  waiting: boolean
  asked: boolean
  onTakeOver: () => void
  onHandBack: (note: string) => void
  onClose: () => void
}

// Chrome is the panel's one head: where the page is, whether the picture
// is live, and who has the browser. In control it becomes a small browser
// toolbar: back, forward, reload and an address you can type into.
export function Chrome({ live, url, control, waiting, asked, onTakeOver, onHandBack, onClose }: Props) {
  return (
    <div className="panel-head bp-head">
      {control && (
        <div className="bp-nav">
          <IconButton size="sm" label="Back" onClick={() => live.send({ kind: 'back' })}>
            <Back size={18} />
          </IconButton>
          <IconButton size="sm" label="Forward" className="bp-fwd" onClick={() => live.send({ kind: 'forward' })}>
            <Fwd size={18} />
          </IconButton>
          <IconButton size="sm" label="Reload" className="bp-reload" onClick={() => live.send({ kind: 'reload' })}>
            <Reload size={17} />
          </IconButton>
        </div>
      )}
      {control ? <AddressField live={live} url={url} /> : <HostPill url={url} />}
      <LivePill live={live} />
      <span className="bp-spacer" />
      {control && coarse && <KeyboardButton live={live} />}
      {control ? (
        <HandBack label={waiting ? 'Hand back' : 'Done'} ask={waiting} onHandBack={onHandBack} />
      ) : (
        // On a phone it's the glyph alone (CSS), its words still its name, so
        // the address keeps the room.
        <Button size="sm" variant={asked ? 'primary' : 'secondary'} className="bp-takeover" icon={<Pointer size={16} />} onClick={onTakeOver} title="Take over">
          <span className="bp-takeover-l">Take over</span>
        </Button>
      )}
      {!control && <FollowChip />}
      {!control && (
        <IconButton size="sm" label="Close the browser" onClick={onClose}>
          <Close size={18} />
        </IconButton>
      )}
      <LoadingBar live={live} />
    </div>
  )
}

// HostPill is where the page is: its icon, its host in ink and the rest of
// the address quieter.
function HostPill({ url }: { url: string }) {
  const a = addressOf(url)
  return (
    <div className="bp-addr" title={url || undefined}>
      {a.blank ? <span className="bp-addr-blank">New tab</span> : <AddressText url={url} />}
    </div>
  )
}

function AddressText({ url }: { url: string }) {
  const a = addressOf(url)
  return (
    <>
      <Favicon host={a.host} size={16} />
      {!a.secure && (
        <span className="bp-insecure" title="Not a private connection">
          <Warning size={14} />
          <span className="sr">Not secure:</span>
        </span>
      )}
      <AddressParts url={url} />
    </>
  )
}

// AddressParts is the address as text: the host, then the rest quieter.
// Short of room, the rest goes first, then what comes before the site
// (hostParts), and the site itself only when it alone doesn't fit, losing
// its start, never its end: the site (evil.io in
// accounts.example.com.evil.io's case) is what tells a page from a
// look-alike, above all on a phone, during a sign-in.
function AddressParts({ url, hidden }: { url: string; hidden?: boolean }) {
  const a = addressOf(url)
  const { sub, site } = hostParts(a.host)
  return (
    <span className={cx('bp-addr-text', hidden && 'bp-addr-over')} aria-hidden={hidden || undefined}>
      <span className="bp-host">
        {sub && (
          <span className="bp-sub">
            <span dir="ltr">{sub}</span>
          </span>
        )}
        <span className="bp-site">
          <span dir="ltr">{site}</span>
        </span>
      </span>
      {a.rest && <span className="bp-rest">{a.rest}</span>}
    </span>
  )
}

// AddressField is the address while you're in control: select it, type
// somewhere else, and Enter goes there. Until then it reads like the host
// pill, host first, without the scheme.
function AddressField({ live, url }: { live: Live; url: string }) {
  const [edit, setEdit] = useState<string | null>(null)
  const a = addressOf(url)
  return (
    <form
      className={cx('bp-addr', 'field-mode', edit !== null && 'editing')}
      onSubmit={(e) => {
        e.preventDefault()
        if (edit && edit.trim()) live.send({ kind: 'navigate', url: edit.trim() })
        setEdit(null)
        ;(e.currentTarget.elements.namedItem('address') as HTMLInputElement | null)?.blur()
        if (!coarse) live.focusKeys()
      }}
    >
      {!a.blank && <Favicon host={a.host} size={16} />}
      <label className="sr" htmlFor="bp-address">
        Address
      </label>
      <span className="bp-addr-in">
        <input
          id="bp-address"
          name="address"
          type="text"
          inputMode="url"
          enterKeyHint="go"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          placeholder="Type an address"
          value={edit ?? (a.blank ? '' : `${a.host}${a.rest}`)}
          onChange={(e) => setEdit(e.target.value)}
          onFocus={(e) => {
            // Shown without its scheme; the whole address comes back to edit.
            const input = e.currentTarget
            setEdit(a.blank ? '' : url)
            requestAnimationFrame(() => input.select())
          }}
          onBlur={() => setEdit(null)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              setEdit(null)
              e.currentTarget.blur()
              if (!coarse) live.focusKeys()
            }
          }}
        />
        {/* Until it's edited, the field reads like the host pill, so a
            narrow one still shows where the host ends (AddressParts). */}
        {edit === null && !a.blank && <AddressParts url={url} hidden />}
      </span>
    </form>
  )
}

const tone: Record<LiveStatus, Tone> = { connecting: 'neutral', live: 'neutral', reconnecting: 'neutral', stale: 'warn', asleep: 'neutral' }
const hint: Record<LiveStatus, string> = {
  connecting: 'Connecting to the browser',
  live: 'You’re seeing the browser live',
  reconnecting: 'The connection dropped; this is the last picture',
  stale: 'No new picture for a few seconds; it may be behind',
  asleep: 'The browser is asleep',
}

// LivePill says whether what you see is the page as it is now.
function LivePill({ live }: { live: Live }) {
  const status = useSelector(live.view, liveStatus)
  return (
    <span className="bp-live" role="status" title={hint[status]}>
      <Pill tone={tone[status]} dot={status !== 'reconnecting' && status !== 'asleep'} pulse={status === 'live'} className={`is-${status}`}>
        {status === 'reconnecting' && <span className="spinner bp-spin" aria-hidden="true" />}
        {status === 'asleep' && <Moon size={12} />}
        <span className="bp-live-label">{statusLabel[status]}</span>
      </Pill>
      <span className="sr">{hint[status]}</span>
    </span>
  )
}

// LoadingBar runs along the foot of the head while a page loads.
function LoadingBar({ live }: { live: Live }) {
  const loading = useSelector(live.view, (v) => v.loading && v.socket === 'open' && !v.asleep)
  return <span className={cx('bp-loading', loading && 'on')} aria-hidden="true" />
}

// KeyboardButton shows the phone's keyboard: it only comes up for a text
// field, and the page's fields are pictures.
function KeyboardButton({ live }: { live: Live }) {
  const { typing } = useSelector(live.view, (v) => ({ typing: v.typing }), shallow)
  return (
    <IconButton size="sm" label="Keyboard" aria-pressed={typing} onPointerDown={(e) => e.preventDefault()} onClick={() => (typing ? live.blurKeys() : live.focusKeys())}>
      <Keyboard size={18} />
    </IconButton>
  )
}
