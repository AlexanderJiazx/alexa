import { Fragment, useState, type CSSProperties, type ReactNode } from 'react'
import { Auth } from '../auth/Auth'
import { Section } from '../dev/Section'
import type { GalleryPage } from '../dev/types'
import { Button } from './Button'
import { Card } from './Card'
import { Chip } from './Chip'
import { ConfirmDialog } from './ConfirmDialog'
import { Dialog } from './Dialog'
import { EmptyState } from './EmptyState'
import { Favicon } from './Favicon'
import { Field, Input, Select, TextArea } from './Field'
import './gallery.css'
import { IconButton } from './IconButton'
import * as icons from './icons'
import { Kbd } from './Kbd'
import { announce, LiveAnnouncer } from './LiveAnnouncer'
import { Mark } from './Mark'
import { Menu, MenuItem, MenuSeparator } from './Menu'
import { Pill } from './Pill'
import { Segmented, Tabs } from './Segmented'
import { Sheet } from './Sheet'
import { Skeleton } from './Skeleton'
import { Timer } from './Timer'
import { Toast } from './Toast'
import { Toggle } from './Toggle'

// The design system's gallery page (?gallery=ui): every token and every
// primitive, in the app's own frames. web/DESIGN.md says how to use them.

const v = (name: string, value: string) => ({ [name]: value }) as CSSProperties

const type: [string, string, string][] = [
  ['display 44/48', 't-display', 'Good evening, Alex'],
  ['head 28/34', 't-head', 'Hacker News, top 5 right now'],
  ['title 20/28', 't-title', 'Which flight should I book?'],
  ['read 17/26', 't-read', 'I found three flights under £200. The 7:40 is the quickest.'],
  ['body 15/22', 't-body', 'Ask Alexa to do anything. Lists, buttons and sheets use this size.'],
  ['small 13/18', 't-small', 'Searched the web · 4 results'],
  ['micro 12/16', 't-micro', 'TODAY 9:41 · 1m 12s'],
]

const swatches: [string, string[]][] = [
  ['Surfaces', ['--bg', '--surface', '--tile']],
  ['Lines', ['--line', '--line-strong', '--focus-line']],
  ['Text', ['--ink', '--ink-2', '--text-2', '--text-3', '--text-4', '--on-ink']],
  ['Status', ['--ok', '--ok-bg', '--warn', '--warn-bg', '--bad', '--bad-bg']],
  ['Series', ['--series-1', '--series-2', '--series-3']],
]

function Theme({ scheme }: { scheme: 'light' | 'dark' }) {
  return (
    <div className={`ug-theme ${scheme}`}>
      <h3>{scheme === 'light' ? 'Light' : 'Dark'}</h3>
      <div className="ug-col">
        {swatches.map(([group, names]) => (
          <div key={group}>
            <p className="eyebrow-caps mt-s2">{group}</p>
            <div className="ug-swatches mt-s2">
              {names.map((n) => (
                <div key={n} className="ug-swatch">
                  <span className="ug-chip" style={v('--sw', `var(${n})`)} />
                  {n}
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="ug-text">
        <p>Ink: what Alexa said.</p>
        <p className="text-2">Text 2: secondary, at least 7:1.</p>
        <p className="text-3">Text 3: tertiary, at least 4.5:1.</p>
        <p className="ug-note">Text 4 is for placeholders only.</p>
      </div>
      <div className="ug-on-s1">
        <p className="t-body">On the surface</p>
        <p className="t-small text-2">Secondary text on a card.</p>
        <p className="t-small text-3">Tertiary text on a card.</p>
      </div>
    </div>
  )
}

function Buttons() {
  const [busy, setBusy] = useState(false)
  return (
    <div className="ug-col">
      <div className="ug-row">
        <Button variant="primary">Book it</Button>
        <Button>Change dates</Button>
        <Button variant="ghost">Not now</Button>
        <Button variant="danger">Delete</Button>
        <Button variant="primary" className="danger">
          Delete for good
        </Button>
      </div>
      <div className="ug-row">
        <Button variant="primary" size="sm">
          Small
        </Button>
        <Button size="sm" icon={<icons.Pointer size={16} />}>
          Take over
        </Button>
        <Button size="lg" variant="primary">
          Large
        </Button>
        <Button disabled>Disabled</Button>
        <Button
          variant="primary"
          loading={busy}
          onClick={() => {
            setBusy(true)
            setTimeout(() => setBusy(false), 1600)
          }}
        >
          Sign in
        </Button>
      </div>
      <div className="ug-row">
        <IconButton label="Reflect">
          <icons.History />
        </IconButton>
        <IconButton label="Download" size="sm" tone="muted">
          <icons.Download size={16} />
        </IconButton>
        <IconButton label="More" size="sm">
          <icons.More size={18} />
        </IconButton>
        <IconButton label="Close" disabled>
          <icons.Close size={18} />
        </IconButton>
      </div>
    </div>
  )
}

function Chips() {
  const [on, setOn] = useState('Daily')
  const [chips, setChips] = useState(['brief.pdf', 'receipt.png'])
  return (
    <div className="ug-col">
      <div className="ug-row">
        {['Research a topic', 'Book or buy…', 'Every morning…', 'Watch a price'].map((t) => (
          <Chip key={t}>{t}</Chip>
        ))}
      </div>
      <div className="ug-row">
        {['Once', 'Daily', 'Weekdays', 'Weekly'].map((t) => (
          <Chip key={t} selected={on === t} onClick={() => setOn(t)}>
            {t}
          </Chip>
        ))}
      </div>
      <div className="ug-row">
        {chips.map((c) => (
          <Chip key={c} icon={<icons.File size={14} />} onRemove={() => setChips(chips.filter((x) => x !== c))}>
            {c}
          </Chip>
        ))}
      </div>
      <div className="ug-row">
        <Pill>3 new</Pill>
        <Pill dot pulse>
          Live
        </Pill>
        <Pill tone="ok">Done</Pill>
        <Pill tone="warn">Paused</Pill>
        <Pill tone="bad">Failed</Pill>

        <Pill tone="ink">admin</Pill>
      </div>
    </div>
  )
}

function Controls() {
  const [seg, setSeg] = useState<'system' | 'light' | 'dark'>('system')
  const [tab, setTab] = useState<'conversation' | 'reports' | 'files'>('conversation')
  const [on, setOn] = useState(true)
  return (
    <div className="ug-col">
      <Segmented
        label="Appearance"
        value={seg}
        onChange={setSeg}
        options={[
          ['system', 'System'],
          ['light', 'Light'],
          ['dark', 'Dark'],
        ]}
      />
      <Tabs
        label="Reflect"
        value={tab}
        onChange={setTab}
        options={[
          ['conversation', 'Conversation'],
          ['reports', 'Reports'],
          ['files', 'Files'],
        ]}
      />
      <Segmented
        label="Size"
        size="sm"
        value={seg}
        onChange={setSeg}
        options={[
          ['system', 'Auto'],
          ['light', 'Sun'],
          ['dark', 'Moon'],
        ]}
      />
      <label className="toggle-row t-body">
        <Toggle checked={on} onChange={setOn} label="Save for next time" />
        Save for next time
      </label>
      {/* Off has to be findable on its own, not only next to on (D7). */}
      <div className="toggle-row t-body">
        <Toggle checked={false} onChange={() => {}} label="Off" />
        Off
        <Toggle checked onChange={() => {}} label="On" />
        On
        <Toggle checked={false} disabled onChange={() => {}} label="Off, disabled" />
        Disabled
      </div>
    </div>
  )
}

function Fields() {
  return (
    <div className="ug-stack">
      <div className="inline">
        <Field label="Name">{(id) => <Input id={id} placeholder="Morning briefing" />}</Field>
        <Field label="When">{(id) => <Input id={id} type="time" defaultValue="07:30" />}</Field>
      </div>
      <Field label="What should Alexa do?" help="Tip: you can also just ask Alexa.">
        {(id) => <TextArea id={id} placeholder="Check my favourite news sites and send me a 5-bullet briefing" />}
      </Field>
      <Field label="Timezone">
        {(id) => (
          <Select id={id} defaultValue="Europe/London">
            <option>Europe/London</option>
            <option>America/New_York</option>
          </Select>
        )}
      </Field>
      <Field label="Password" error="That password is too short.">
        {(id) => <Input id={id} type="password" defaultValue="hunter" />}
      </Field>
    </div>
  )
}

function Overlays() {
  const [dialog, setDialog] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [sheet, setSheet] = useState(false)
  const [menu, setMenu] = useState(false)
  return (
    <div className="ug-menu-host">
      <div className="ug-row">
        <Button onClick={() => setDialog(true)}>Dialog</Button>
        <Button variant="danger" onClick={() => setConfirm(true)}>
          Confirm
        </Button>
        <Button onClick={() => setSheet(true)}>Sheet</Button>
        <Button aria-expanded={menu} onClick={() => setMenu(!menu)} icon={<icons.More size={18} />}>
          Menu
        </Button>
      </div>
      {menu && (
        <Menu label="Report" onClose={() => setMenu(false)}>
          <MenuItem icon={<icons.Copy size={18} />} hint="⌘C" onSelect={() => setMenu(false)}>
            Copy as markdown
          </MenuItem>
          <MenuItem icon={<icons.Download size={18} />} onSelect={() => setMenu(false)}>
            Download
          </MenuItem>
          <MenuItem icon={<icons.Bell size={18} />} count={3} onSelect={() => setMenu(false)}>
            Notifications
          </MenuItem>
          <MenuSeparator />
          <MenuItem icon={<icons.Trash size={18} />} tone="danger" onSelect={() => setMenu(false)}>
            Delete
          </MenuItem>
        </Menu>
      )}
      <Dialog open={dialog} onClose={() => setDialog(false)} title="Add to calendar?" actions={<Button variant="primary" onClick={() => setDialog(false)}>Add 3 events</Button>}>
        Your Lisbon trip has three events: two flights and the hotel check-in.
      </Dialog>
      <ConfirmDialog open={confirm} title="Delete this login?" body="Alexa won’t be able to sign in to amazon.com for you any more." confirmLabel="Delete" tone="danger" onConfirm={() => setConfirm(false)} onCancel={() => setConfirm(false)} />
      <Sheet open={sheet} onClose={() => setSheet(false)} title="Memory" actions={<Button size="sm">Add</Button>}>
        <p className="intro">Things Alexa remembers about you, for good.</p>
        <ul className="list">
          <li>
            <div className="main">
              <div className="t1 plain">I’m vegetarian</div>
              <div className="t2">Learned Tue</div>
            </div>
          </li>
          <li>
            <div className="main">
              <div className="t1 plain">Prefers aisle seats on flights</div>
              <div className="t2">Learned 2 weeks ago</div>
            </div>
          </li>
        </ul>
      </Sheet>
    </div>
  )
}

function Feedback() {
  const [from] = useState(() => Date.now() - 72_000)
  return (
    <div className="ug-col">
      <div className="ug-toasts">
        <Toast title="Saved" tone="ok" onClose={() => {}} />
        <Toast title="Couldn’t send" body="The server didn’t answer. Try again in a moment." tone="bad" action={<Button size="sm">Retry</Button>} onClose={() => {}} />
        <Toast title="Memory forgotten" action={<Button size="sm" variant="ghost">Undo</Button>} />
        <Toast title="Report ready" body="Hacker News, top 5 right now" />
      </div>
      <div className="ug-row">
        <Button onClick={() => announce('Report ready: Hacker News, top 5')}>Announce politely</Button>
        <span className="ug-note">Screen readers hear it; nothing shows.</span>
      </div>
      <LiveAnnouncer />
      <div className="ug-row t-small text-2">
        Working for <Timer from={from} className="text-3" /> · finished in <Timer from={0} to={49_000} /> · <Timer from={0} to={4_380_000} />
      </div>
      <div className="ug-row t-small text-3">
        <Kbd>⌘K</Kbd> commands <Kbd>↵</Kbd> send <Kbd>Esc</Kbd> stop
      </div>
    </div>
  )
}

function Loading() {
  return (
    <div className="ug-stack">
      <Card>
        <div className="ug-row">
          <Skeleton width={40} height={48} radius="xs" />
          <div className="ug-col ug-grow">
            <Skeleton width="70%" height={14} />
            <Skeleton width="40%" height={12} />
          </div>
        </div>
      </Card>
      <Skeleton lines={3} />
      <div className="ug-row">
        <span className="spinner" />
        <span className="ug-note">Spinner, when the shape isn’t known.</span>
      </div>
    </div>
  )
}

function Cards() {
  return (
    <div className="ug-stack">
      <Card>
        <p className="t-body">A card sits on the page: the surface and a hairline.</p>
        <p className="t-small text-3 mt-s2">Report · 5 sections · 2 min read</p>
      </Card>
      <Card interactive tabIndex={0}>
        <p className="t-body">An interactive card darkens its frame on hover and presses in.</p>
      </Card>
      <Card>
        <EmptyState icon={<icons.KeyIcon size={28} />} title="No saved logins yet" body="When Alexa needs to sign in somewhere, it asks you here, and you can save the login for next time." action={<Button variant="primary">Add a login</Button>} />
      </Card>
    </div>
  )
}

function Sites() {
  const hosts = ['news.ycombinator.com', 'en.wikipedia.org', 'github.com', 'mistral.ai', 'nonexistent.invalid']
  return (
    <div className="ug-col">
      <div className="ug-row">
        {hosts.map((h) => (
          <span key={h} className="ug-row t-small text-2">
            <Favicon host={h} /> {h}
          </span>
        ))}
      </div>
      <div className="ug-row">
        <span className="favicons">
          {hosts.slice(0, 4).map((h) => (
            <Favicon key={h} host={h} size={20} round />
          ))}
        </span>
        <Favicon host="example.com" size={32} />
        <Favicon host="mistral.ai" size={32} />
      </div>
    </div>
  )
}

function Icons() {
  const named = Object.entries(icons).filter(([n, C]) => typeof C === 'function' && /^[A-Z]/.test(n) && !['CursorArrow', 'ToolGlyph'].includes(n)) as [string, (p: { size?: number }) => ReactNode][]
  return (
    <div className="ug-col">
      <p className="eyebrow-caps">Tool kinds</p>
      <div className="ug-icons">
        {Object.keys(icons.toolGlyphs).map((k) => (
          <div key={k}>
            <icons.ToolGlyph kind={k} size={20} />
            <span>{k}</span>
          </div>
        ))}
      </div>
      <p className="eyebrow-caps mt-s4">Everything else</p>
      <div className="ug-icons">
        {named.map(([n, C]) => (
          <div key={n}>
            <C size={20} />
            <span>{n}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

const page: GalleryPage = {
  name: 'ui',
  title: 'Design system',
  render: () => (
    <>
      <Section title="Type scale">
        <dl className="ug-type">
          {type.map(([label, cls, sample]) => (
            <Fragment key={cls}>
              <dt>{label}</dt>
              <dd className={cls}>{sample}</dd>
            </Fragment>
          ))}
          <dt>display italic</dt>
          <dd className="t-display ug-italic">a quiet, editorial voice</dd>
          <dt>mono 13</dt>
          <dd className="t-small ug-mono">
            0 8 * * 1-5 · 21a94755
          </dd>
        </dl>
      </Section>
      <Section title="Colours, both themes">
        <div className="ug-themes">
          <Theme scheme="light" />
          <Theme scheme="dark" />
        </div>
      </Section>

      <Section title="Radii and spacing">
        <div className="ug-row">
          <div className="ug-radii">
            {['xs', 'sm', 'md', 'lg', 'xl', 'full'].map((r) => (
              <div key={r} style={v('--r', `var(--r-${r})`)}>
                {r}
              </div>
            ))}
          </div>
          <div className="ug-space">
            {[1, 2, 3, 4, 5, 6, 8, 10, 14].map((n) => (
              <div key={n}>
                <i style={v('--w', `var(--s${n})`)} />s{n}
              </div>
            ))}
          </div>
        </div>
      </Section>
      <Section title="Presence: the dot">
        <div className="ug-marks">
          <div>
            <Mark /> Idle
          </div>
          <div>
            <Mark state="working" /> Working
          </div>
          <div>
            <Mark state="waiting" /> You’re in control
          </div>
          <div>
            <Mark state="needs" /> Needs you
          </div>
          <div>
            <Mark size="lg" draw /> Drawn in
          </div>
          <div>
            <Mark size="sm" state="working" />
            <span className="shimmer t-body">Checking top stories</span>
          </div>
        </div>
      </Section>
      <Section title="Buttons">
        <Buttons />
      </Section>
      <Section title="Chips and pills">
        <Chips />
      </Section>
      <Section title="Segmented, tabs and toggle">
        <Controls />
      </Section>
      <Section title="Fields">
        <Fields />
      </Section>
      <Section title="Cards and empty states">
        <Cards />
      </Section>
      <Section title="Dialog, sheet and menu">
        <Overlays />
      </Section>
      <Section title="Toasts, timers, announcer, keys">
        <Feedback />
      </Section>
      <Section title="Loading">
        <Loading />
      </Section>
      <Section title="Favicons">
        <Sites />
      </Section>
      <Section title="Icons">
        <Icons />
      </Section>
      <Section title="Sign-in">
        <div className="ug-screen">
          <Auth needsSetup={false} signupAllowed onDone={() => {}} />
        </div>
      </Section>
    </>
  ),
}

export default page
