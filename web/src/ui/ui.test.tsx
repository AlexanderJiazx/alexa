import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { Button, buttonClass } from './Button'
import { Card, cardClass } from './Card'
import { Chip } from './Chip'
import { ConfirmDialog } from './ConfirmDialog'
import { Dialog } from './Dialog'
import { EmptyState } from './EmptyState'
import { Favicon, hostName, letterFor } from './Favicon'
import { Field, Input } from './Field'
import { IconButton } from './IconButton'
import { Kbd } from './Kbd'
import { LiveAnnouncer } from './LiveAnnouncer'
import { Mark } from './Mark'
import { Menu, MenuItem, MenuSeparator } from './Menu'
import { Pill } from './Pill'
import { Segmented, Tabs } from './Segmented'
import { Sheet, shouldClose } from './Sheet'
import { Skeleton } from './Skeleton'
import { Timer, fmtDuration } from './Timer'
import { Toast } from './Toast'
import { Toggle } from './Toggle'
import { moveIndex, trapIndex } from './focus'
import { closeOnce, closing } from './useModal'
import { Search, ToolGlyph } from './icons'

// The primitives render without a DOM, so these check the markup each one
// promises (classes, roles, labels); behaviour that needs a browser (focus,
// <dialog>) was checked by hand in the gallery.
const html = renderToStaticMarkup
const noop = () => {}

// M8: Escape on a modal asks its owner to close once, however the browser
// fires it.
describe('closeOnce', () => {
  const escape = (cancelable: boolean) => {
    let prevented = false
    return {
      preventDefault: () => {
        if (cancelable) prevented = true
      },
      get defaultPrevented() {
        return prevented
      },
    }
  }

  it('asks once when the browser lets cancel be stopped', () => {
    let asks = 0
    const once = closeOnce(() => asks++, () => true)
    once.cancel(escape(true))
    expect(asks).toBe(1)
  })

  it('asks once when the browser closes the dialog after a cancel it wouldn’t stop', () => {
    let asks = 0
    const once = closeOnce(() => asks++, () => true)
    once.cancel(escape(false))
    once.closed()
    expect(asks).toBe(1)
    // The next close the browser makes on its own asks again.
    once.closed()
    expect(asks).toBe(2)
  })

  it('asks for a close with no cancel, only while the owner has it open', () => {
    let asks = 0
    let open = true
    const once = closeOnce(() => asks++, () => open)
    once.closed()
    expect(asks).toBe(1)
    open = false
    once.closed()
    expect(asks).toBe(1)
  })
})

// FH-8: closed by the browser itself (Escape with no gesture since the last
// one, as from a notification click), Reflect stayed shown: no focus back,
// and the next open wasn't taken for one.
describe('closing', () => {
  it('plays the exit while the dialog is still up', () => {
    expect(closing(false, true, true)).toBe('play')
  })
  it('puts it away at once when the browser already closed it', () => {
    expect(closing(false, false, true)).toBe('now')
  })
  it('does nothing while open, or once put away', () => {
    expect(closing(true, true, true)).toBeNull()
    expect(closing(true, false, true)).toBeNull()
    expect(closing(false, false, false)).toBeNull()
  })
})

describe('focus', () => {
  it('moves through a list and wraps at the ends', () => {
    expect(moveIndex('ArrowDown', 0, 3)).toBe(1)
    expect(moveIndex('ArrowDown', 2, 3)).toBe(0)
    expect(moveIndex('ArrowUp', 0, 3)).toBe(2)
    expect(moveIndex('Home', 2, 3)).toBe(0)
    expect(moveIndex('End', 0, 3)).toBe(2)
  })

  it('starts from the right end when nothing has focus', () => {
    expect(moveIndex('ArrowDown', -1, 3)).toBe(0)
    expect(moveIndex('ArrowUp', -1, 3)).toBe(2)
  })

  it('only answers the keys of its orientation', () => {
    expect(moveIndex('ArrowRight', 0, 3, 'vertical')).toBeNull()
    expect(moveIndex('ArrowDown', 0, 3, 'horizontal')).toBeNull()
    expect(moveIndex('ArrowRight', 0, 3, 'horizontal')).toBe(1)
    expect(moveIndex('ArrowLeft', 0, 3, 'both')).toBe(2)
    expect(moveIndex('a', 0, 3)).toBeNull()
    expect(moveIndex('ArrowDown', 0, 0)).toBeNull()
  })

  it('keeps Tab inside a trap', () => {
    expect(trapIndex(2, 3, false)).toBe(0)
    expect(trapIndex(0, 3, true)).toBe(2)
    expect(trapIndex(1, 3, false)).toBeNull()
    expect(trapIndex(-1, 3, false)).toBe(0)
    expect(trapIndex(-1, 3, true)).toBe(2)
    expect(trapIndex(0, 0, false)).toBeNull()
  })
})

describe('fmtDuration', () => {
  it('reads like speech', () => {
    expect(fmtDuration(0)).toBe('0s')
    expect(fmtDuration(8_400)).toBe('8s')
    expect(fmtDuration(72_000)).toBe('1m 12s')
    expect(fmtDuration(120_000)).toBe('2m')
    expect(fmtDuration(3_900_000)).toBe('1h 5m')
    expect(fmtDuration(7_200_000)).toBe('2h')
    expect(fmtDuration(-5)).toBe('0s')
  })
})

describe('Favicon helpers', () => {
  it('finds the bare host', () => {
    expect(hostName('https://www.bbc.co.uk/news')).toBe('bbc.co.uk')
    expect(hostName('news.ycombinator.com')).toBe('news.ycombinator.com')
    expect(hostName(' www.example.com ')).toBe('example.com')
  })

  it('letters the site, not its subdomain', () => {
    expect(letterFor('github.com')).toBe('G')
    expect(letterFor('en.wikipedia.org')).toBe('W')
    expect(letterFor('m.youtube.com')).toBe('Y')
    expect(letterFor('')).toBe('?')
  })
})

describe('primitives', () => {
  it('Button: secondary by default, a spinner while loading', () => {
    expect(buttonClass()).toBe('btn')
    expect(buttonClass('primary', 'lg', true)).toBe('btn primary lg block')
    expect(html(<Button>Save</Button>)).toBe('<button type="button" class="btn">Save</button>')
    const busy = html(
      <Button variant="primary" loading>
        Save
      </Button>,
    )
    expect(busy).toContain('aria-busy="true"')
    expect(busy).toContain('disabled=""')
    expect(busy).toContain('<span class="sr">Save</span>')
  })

  it('IconButton always has a label', () => {
    const out = html(
      <IconButton label="Close" size="sm" tone="muted">
        <Search />
      </IconButton>,
    )
    expect(out).toContain('class="icon-btn sm muted"')
    expect(out).toContain('aria-label="Close"')
    expect(out).toContain('title="Close"')
  })

  it('Pill, Chip and Kbd', () => {
    expect(html(<Pill tone="ink" dot pulse>Live</Pill>)).toBe('<span class="pill ink dot pulse">Live</span>')
    const chip = html(
      <Chip selected onRemove={noop}>
        Report
      </Chip>,
    )
    expect(chip).toContain('class="chip-group"')
    expect(chip).toContain('aria-pressed="true"')
    expect(chip).toContain('aria-label="Remove"')
    expect(html(<Kbd>⌘K</Kbd>)).toBe('<kbd class="kbd">⌘K</kbd>')
  })

  it('Card and EmptyState', () => {
    expect(cardClass(true, 'lg', true)).toBe('card interactive pad-lg flat')
    expect(html(<Card>x</Card>)).toBe('<div class="card">x</div>')
    const empty = html(<EmptyState icon={<Search />} title="No reports yet" body="They’ll appear here." />)
    expect(empty).toContain('class="empty-glyph" aria-hidden="true"')
    expect(empty).toContain('<p class="empty-title">No reports yet</p>')
  })

  it('Mark: one dot, four presences', () => {
    expect(html(<Mark />)).toBe('<span class="mark" aria-hidden="true"></span>')
    expect(html(<Mark state="working" size="sm" />)).toContain('class="mark busy sm"')
    expect(html(<Mark state="waiting" />)).toContain('class="mark wait"')
    expect(html(<Mark state="needs" label="Alexa needs you" />)).toContain('role="img" aria-label="Alexa needs you"')
  })

  it('Segmented is a radio group; Tabs a tab list', () => {
    const opts = [
      ['a', 'One'],
      ['b', 'Two'],
    ] as const
    const seg = html(<Segmented value="b" options={opts} onChange={noop} label="Pick" />)
    expect(seg).toContain('role="radiogroup" aria-label="Pick"')
    expect(seg).toContain('role="radio" aria-checked="true" tabindex="0"')
    expect(seg).toContain('role="radio" aria-checked="false" tabindex="-1"')
    const tabs = html(<Tabs value="a" options={opts} onChange={noop} label="View" block />)
    expect(tabs).toContain('class="seg block" role="tablist"')
    expect(tabs).toContain('role="tab" aria-selected="true"')
  })

  it('Toggle is a switch', () => {
    expect(html(<Toggle checked onChange={noop} label="Email me" />)).toBe('<button type="button" role="switch" class="toggle" aria-checked="true" aria-label="Email me"></button>')
  })

  it('Field ties its label to the control and shows an error over help', () => {
    const out = html(
      <Field label="Name" id="n" help="As Alexa should say it." error="Required">
        {(id) => <Input id={id} />}
      </Field>,
    )
    expect(out).toContain('<label for="n">Name</label><input class="input" id="n" aria-describedby="n-msg" aria-invalid="true"/>')
    // Read with the field, not shouted as an alert of its own.
    expect(out).toContain('<span id="n-msg" class="err">Required</span>')
    expect(out).not.toContain('role="alert"')
    expect(out).not.toContain('As Alexa should say it.')
  })

  it('Field describes its control with its help, and leaves other controls alone', () => {
    const out = html(
      <Field label="Name" id="n" help="As Alexa should say it.">
        {(id, describedBy) => (
          <>
            <Input id={id} aria-describedby="extra" />
            <Input id="other" />
            <span data-by={describedBy} />
          </>
        )}
      </Field>,
    )
    expect(out).toContain('<input class="input" id="n" aria-describedby="extra n-msg"/>')
    expect(out).toContain('<input class="input" id="other"/>')
    expect(out).toContain('<span data-by="n-msg"></span><span id="n-msg" class="help">As Alexa should say it.</span>')
    expect(html(<Input id="n" />)).toBe('<input class="input" id="n"/>')
  })

  it('Skeleton draws lines, the last one shorter', () => {
    const out = html(<Skeleton lines={3} />)
    expect(out.match(/class="skel line"/g)).toHaveLength(3)
    expect(out).toContain('--w:62%')
    expect(html(<Skeleton width={120} height={16} radius="full" />)).toContain('class="skel r-full"')
  })

  it('Timer shows a finished duration in tabular figures', () => {
    expect(html(<Timer from={0} to={72_000} />)).toBe('<time class="timer" dateTime="PT72S">1m 12s</time>')
  })

  it('Toast: read out as a status, or an alert when something failed', () => {
    // The live region arrives empty and is filled a moment later, so it's
    // read; the words on screen aren't live, so they aren't read twice.
    const ok = html(<Toast title="Saved" tone="ok" />)
    expect(ok).toContain('<div class="toast ok"><span class="sr" role="status"></span><span class="toast-ic" aria-hidden="true">')
    expect(html(<Toast title="Forgotten" />)).toContain('<span class="toast-ic" aria-hidden="true"><svg')
    const bad = html(<Toast title="Couldn’t save" tone="bad" onClose={noop} />)
    expect(bad).toContain('<span class="sr" role="alert"></span>')
    expect(bad).toContain('aria-label="Dismiss"')
  })

  it('Sheet goes when pulled far enough down, or flung', () => {
    expect(shouldClose(40, 0, 700)).toBe(false)
    expect(shouldClose(200, 0, 700)).toBe(true)
    // A short sheet needs less: a third of its height.
    expect(shouldClose(100, 0, 300)).toBe(true)
    // A quick flick carries it on.
    expect(shouldClose(40, 1, 700)).toBe(true)
    // Let go on the way back up, it stays.
    expect(shouldClose(150, -0.5, 700)).toBe(false)
  })

  it('Favicon starts as a letter tile', () => {
    const out = html(<Favicon host="https://www.github.com/x" />)
    expect(out).toContain('<span class="favicon-letter">G</span>')
    expect(out).toContain('src="/api/favicon/github.com"')
  })

  it('Menu, its items and separators', () => {
    const out = html(
      <Menu label="Account" onClose={noop}>
        <MenuItem onSelect={noop} count={3}>
          Notifications
        </MenuItem>
        <MenuSeparator />
        <MenuItem onSelect={noop} tone="quiet">
          Sign out
        </MenuItem>
      </Menu>,
    )
    expect(out).toContain('class="menu" role="menu" aria-label="Account"')
    expect(out).toContain('<span class="count">3</span>')
    expect(out).toContain('role="separator"')
    expect(out).toContain('class="menu-item quiet"')
  })

  it('Dialog, ConfirmDialog and Sheet render only while open', () => {
    expect(html(<Dialog open={false} onClose={noop} title="Hi" />)).toBe('')
    const dialog = html(<Dialog open onClose={noop} title="Clear the conversation?" actions={<Button>OK</Button>} />)
    expect(dialog).toMatch(/^<dialog class="dialog" aria-labelledby="([^"]+)"><div class="dialog-head"><h2 id="\1">/)
    const confirm = html(<ConfirmDialog open title="Forget this?" tone="danger" confirmLabel="Forget" onConfirm={noop} onCancel={noop} />)
    // Cancel comes first, so Enter never destroys anything.
    expect(confirm.indexOf('Cancel')).toBeLessThan(confirm.indexOf('Forget<'))
    expect(confirm).toContain('class="btn primary danger"')
    const sheet = html(
      <Sheet open onClose={noop} title="Settings">
        body
      </Sheet>,
    )
    expect(sheet).toContain('class="sheet"')
    expect(sheet).toContain('aria-label="Close"')
    expect(sheet).toContain('<div class="sheet-body">body</div>')
  })

  it('LiveAnnouncer has a polite and an assertive region', () => {
    const out = html(<LiveAnnouncer />)
    expect(out).toContain('role="status" aria-live="polite"')
    expect(out).toContain('role="alert" aria-live="assertive"')
  })

  it('ToolGlyph falls back for a kind it doesn’t know', () => {
    expect(html(<ToolGlyph kind="search" />)).toContain('<svg')
    expect(html(<ToolGlyph kind="nonsense" />)).toContain('<svg')
  })
})
