import { useState } from 'react'
import { HeaderBar } from '../app/Header'
import { Auth } from '../auth/Auth'
import { Section } from '../dev/Section'
import type { GalleryPage } from '../dev/types'
import { buildCommands } from '../features/palette/commands'
import { filterCommands } from '../features/palette/filter'
import { PaletteBody } from '../features/palette/Palette'
import { ReflectPage } from '../reflect/Reflect'
import { blankSession } from '../state/reducer'
import { setApp, store } from '../state/store'
import { Button } from '../ui/Button'
import { Toast, ToastStack } from '../ui/Toast'
import { AccountMenu } from './AccountMenu'
import { fakeApi, feed, me, notes, reports } from './fixtures'
import './gallery.css'
import { NotificationsSheet } from './NotificationsSheet'
import { NewSchedule } from './SchedulesPane'
import { SettingsInline } from './Settings'
import { InlineSheets } from './Sheet'

// The chrome's gallery page (WP-12): the header in each state, the account
// menu, the toasts, the command palette, Settings at each of its sections
// (the frames at 380 and 680 show it as a phone does), the Notifications
// sheet, Reflect and sign-in, from fixtures and with no backend.

let seeded = false
function seed(empty: boolean) {
  if (seeded) return
  seeded = true
  fakeApi(empty)
  // Live, so the header shows no "Reconnecting…" with no server behind it.
  setApp({ user: me, boot: 'ready', connection: 'live', config: { model: 'meta/llama-5-large', viewport: { width: 1280, height: 800 }, emailEnabled: false, shellEnabled: false, sandboxed: true, vapidPublicKey: '' } })
  const d = store.get().data
  store.set((s) => ({ ...s, data: { ...d, loaded: true, feed: empty ? [] : feed, notes: empty ? [] : notes, session: { ...blankSession, status: 'working', activity: 'Reading bbc.co.uk', contextTokens: 41200, compactions: 2 } } }))
}

const noop = () => {}

function PaletteDemo() {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const all = buildCommands({ status: 'working', control: false, surface: '', reflect: false, isAdmin: true, reports })
  const shown = filterCommands(all, query)
  return (
    <div className="g-palette">
      <div className="palette">
        <PaletteBody
          commands={shown}
          query={query}
          active={Math.min(active, shown.length - 1)}
          onQuery={(q) => {
            setQuery(q)
            setActive(0)
          }}
          onActive={setActive}
          onRun={noop}
        />
      </div>
    </div>
  )
}

// Settings as it opens (General on a wide screen, the list on a phone),
// searching, and at each section.
function SettingsDemo() {
  return (
    <div className="g-stack">
      <SettingsInline />
      <SettingsInline query="dark mode" />
      <SettingsInline query="zebra" />
      <SettingsInline initial="notifications" />
      <SettingsInline initial="memory" />
      <SettingsInline initial="logins" />
      <SettingsInline initial="schedules" />
      <div className="g-pane settings-body">
        <NewSchedule onDone={noop} onCancel={noop} />
      </div>
      <SettingsInline initial="conversation" />
      <SettingsInline initial="account" />
      <SettingsInline initial="users" />
    </div>
  )
}

function Inbox() {
  return (
    <InlineSheets.Provider value={true}>
      <NotificationsSheet open onClose={noop} />
    </InlineSheets.Provider>
  )
}

const page: GalleryPage = {
  name: 'chrome',
  title: 'Chrome',
  render() {
    seed(false)
    return (
      <div className="g-chrome">
        <Section title="Header: idle, working, needs you, in control">
          <div className="g-head">
            <HeaderBar presence="idle" title="" name="Alexander" unread={0} badge={false} menu={false} />
          </div>
          <p className="g-cap" />
          <div className="g-head">
            <HeaderBar presence="working" title="Find me the best noise-cancelling headphones" name="Alexander" unread={0} badge={false} menu={false} />
          </div>
          <p className="g-cap" />
          <div className="g-head">
            <HeaderBar presence="needs" title="Morning briefing · scheduled" name="Alexander" unread={2} badge menu={false} />
          </div>
          <p className="g-cap" />
          <div className="g-head">
            <HeaderBar presence="waiting" title="Eurostar fares for December" name="Alexander" unread={0} badge={false} menu />
          </div>
        </Section>

        <Section title="Account menu">
          <div className="g-menu">
            <AccountMenu user={me} onClose={noop} />
          </div>
        </Section>

        <Section title="Toasts">
          <div className="g-toasts">
            <ToastStack>
              <Toast
                title="Forgotten"
                body="Vegetarian, and doesn’t eat fish either."
                onClose={noop}
                action={
                  <Button size="sm" variant="ghost" className="toast-act">
                    Undo
                  </Button>
                }
              />
              <Toast title="Scheduled" body="Weekdays at 7:30 AM" tone="ok" onClose={noop} />
              <Toast title="Couldn’t send" body="The server didn’t answer. Your message is still in the box." tone="bad" onClose={noop} />
              <Toast title="Notifications are on" onClose={noop} />
            </ToastStack>
          </div>
        </Section>

        <Section title="Command palette (⌘K), while Alexa works">
          <PaletteDemo />
        </Section>

        <Section title="Settings">
          <SettingsDemo />
        </Section>

        <Section title="Notifications">
          <Inbox />
        </Section>

        <Section title="Reflect: the conversation">
          <div className="g-frame-box">
            <ReflectPage show="conversation" />
          </div>
        </Section>
        <Section title="Reflect: reports">
          <div className="g-frame-box short">
            <ReflectPage show="reports" />
          </div>
        </Section>
        <Section title="Reflect: files">
          <div className="g-frame-box short">
            <ReflectPage show="files" />
          </div>
        </Section>

        <Section title="Sign in">
          <div className="g-frame-box short">
            <Auth needsSetup={false} signupAllowed onDone={noop} />
          </div>
        </Section>
      </div>
    )
  },
}
export default page

// firstRunPage is the same chrome with nothing in it yet: every empty
// state (reflect/gallery.tsx shows it).
export function firstRunPage(): GalleryPage {
  return {
    name: 'chrome-empty',
    title: 'Chrome, first run',
    render() {
      seed(true)
      return (
        <div className="g-chrome">
          <Section title="Settings and Notifications, empty">
            <div className="g-stack">
              <SettingsInline initial="memory" />
              <SettingsInline initial="schedules" />
              <SettingsInline initial="logins" />
              <Inbox />
            </div>
          </Section>
          <Section title="Reflect, empty">
            <div className="g-row">
              <div className="g-frame-box short">
                <ReflectPage show="conversation" />
              </div>
              <div className="g-frame-box short">
                <ReflectPage show="reports" />
              </div>
              <div className="g-frame-box short">
                <ReflectPage show="files" />
              </div>
            </div>
          </Section>
          <Section title="Set up">
            <div className="g-frame-box short">
              <Auth needsSetup signupAllowed={false} onDone={noop} />
            </div>
          </Section>
        </div>
      )
    },
  }
}
