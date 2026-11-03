# Alexa web: architecture

How the frontend is put together, where new work plugs in, and who owns
which files. Read this before you add a feature. It follows SYNTHESIS.md 5.1
(WP-02).

The short version:

- Server state lives in one external store, through a pure reducer.
- One connection manager keeps the WebSocket and the snapshot in step.
- Components read what they need through selector hooks, and change things through actions.
- New kinds of content plug in through registries.
- New UI goes into mount points that already exist.

## The tree

```
web/
  ARCHITECTURE.md
  public/                     icon, manifest, service worker, alexa-base.css (the base styles for HTML reports)
  src/
    main.tsx                  global CSS in order, then the app, or the dev gallery (?gallery)
    api/
      types.ts                REST types: Entry, SessionState (View), Report, Needs, ... (additive only)
      blocks.ts               generative UI blocks, Appendix A.2 (snake_case fields)
      progress.ts             plan, step data, run events and failures, Appendix B.1 to B.4
      realtime.ts             the WebSocket protocol, Appendix B.5: envelope, hello, resync, attention, 4401
      client.ts               every endpoint, plus URL helpers (files, favicons, images, shots)
    state/
      store.ts                createStore, useSelector, shallow; the app store, dispatch, setUI, setApp
      reducer.ts              pure reducer for server data: snapshot buffering, upsert, merge (tested)
      selectors.ts            selector hooks (useSession, useFeed, useNeeds, useUI, ...)
      connection.ts           the socket: generations, hello sync, ?after, gaps, resync, 4401 (tested)
      exchange.ts             what the main screen shows, and putting an exchange away (tested)
      actions.ts              everything the UI does: send, answer, show, open a sheet, ...
      live.ts                 tiny stores for hot paths: browser cursor, URL and crash count, voice partials
      *.test.ts               vitest specs
    app/
      App.tsx                 boot, sign-in and the shell
      Header.tsx              mark, connection pill, Reflect, avatar
      Stage.tsx               exchange column, panel slot, needs dock, composer
      OverlayHost.tsx         Reflect, account menu, sheets, toasts
      layout.css
    features/
      conversation/           Log, entry registry, entries/*, Suggestions, linkify, fixtures, gallery
      progress/               RunCard, StepsBlock, WorkingLine, progress.css
      blocks/                 BlockList, Sources, fixtures (A.6 and one per type), gallery, blocks.css
      home/                   HomeBoard and its cards (needs, resume, upcoming, recent reports, try chips), prefill.ts, home.css
      ask/                    AskCard, ConfirmCard, AskForm, MultiChips, FailureCard, StoppedCard, NeedsDock, LoginForm, ask.css
      caption/                Caption (what Alexa said, under a panel on small screens)
      palette/                Palette (the command palette)
    composer/                 Composer, outbox.ts (clientId, interrupt, queued), focus.ts (focus and prefill), VoiceTray, ConnectionPill, voice.ts, spoken.ts, composer.css
    panels/
      PanelHost.tsx           the one panel for the surface (each panel mounts once)
      PanelSheet.tsx          the same panel as a sheet on phones
      registry.ts             surface kind to panel
      browser/                BrowserPanel, LiveScreen, browser.css
      report/                 ReportPanel, html.ts (HTML report sandboxing), report.css
      entry/                  EntryPanel (an entry's blocks, expanded)
    sheets/                   Settings (the window) and one pane per section, the Notifications sheet, AccountMenu, settingsIndex.ts (search), registry, cron.ts, push.ts, sheets.css
    reflect/                  Reflect, History, PastAsk, ReportsTab, FilesTab, exchanges.ts, reflect.css
    auth/                     Auth (sign-in, setup, sign-up)
    ui/                       the design system's primitives (Button, Card, Sheet, Toast, ...), icons.tsx, ui.css
    lib/                      format.ts, url.ts, theme.ts, useExit.ts
    styles/                   tokens.css, base.css (which imports ui/ui.css)
    dev/                      the gallery (development only)
```

## How data flows

```
 REST (api/client.ts) ──┐
                        ├─> connection.ts ─> dispatch(Action) ─> reducer.ts ─> store
 WebSocket (/api/ws) ───┘                                                          │
                                                                                   v
 components <── selector hooks (selectors.ts, useApp) <────────────────────────────┘
     │
     └─> actions.ts ─> api/client.ts (and optimistic dispatch / setUI)
```

- **Server data** lives under `store.get().data` (type `Data` in reducer.ts):
  - the session, the feed and hasMore
  - reports and notifications
  - the latest run event
  - change counters, and the sync bookkeeping

  Only the reducer changes it.
- **App state** sits next to it in `AppState`:
  - boot, setup, the user and the server config
  - the connection status
  - `ui`: the menu, the sheet, Reflect and its tab, voice, and toasts
- **Hot paths** don't touch the app store (FE-06):
  - Browser cursor and URL events go to `liveStore`. Only LiveScreen re-renders.
  - Voice partials go to `heardStore`. Only VoiceTray re-renders.

### Snapshot and live events (FE-01)

When the connection syncs it dispatches `syncStart` and fetches the snapshot. Events that arrive meanwhile go into `sync.buffer`. When the snapshot lands, the buffer is replayed on top of it:

- When the snapshot and an event both carry `seq`, only events after the snapshot's seq apply.
- Without seq, every buffered event is replayed through the newer-wins upsert. That means `rev` when both copies have one, otherwise `updatedAt`, and on a tie the incoming copy wins.
- A `reset` that arrived mid-fetch sets `sync.again`, and the connection fetches again.
- A failed snapshot still applies the buffer.

`mergeFeed` (FE-12) keeps the older history the user loaded, but only when the snapshot overlaps it. Otherwise the snapshot replaces the feed.

### The connection (state/connection.ts)

- **One socket per generation (FE-02).** Each open bumps a generation counter. Handlers from an older socket are ignored, so a socket that is still closing can't schedule a second reconnect.
- **Status.** One of `connecting | live | reconnecting | offline`, read with `useConnection()`.
- **Sync on hello.** The server sends `hello {epoch, seq}` after it subscribes the socket. The client then fetches the snapshot, so no event can fall between the two. If no hello comes within 1.5 s (an older server), it syncs anyway.
- **Reconnecting.**
  - It reconnects with `/api/ws?after=<epoch>:<seq>` once it knows them.
  - If the hello has the same epoch, the server replays and there's no snapshot. A new epoch means a restart, so it takes a snapshot.
  - A gap in `seq`, or a `resync` event, refetches the snapshot.
  - Backoff is `min(15 s, 0.5 s × 2^n)`.
  - Close code 4000 (`closeLagged`) means the server dropped a socket that fell behind. The client reconnects at once with its cursor, and the server replays what it missed or sends `resync`.
- **Attention.**
  - The client sends `{type:"attention", visible, focused}` and the legacy `{type:"visibility", visible}` on open and whenever either changes.
  - The server counts attention for two minutes only, so the client repeats it every 45 s while the socket is open.
- **Revoked sessions.**
  - Close code 4401 means the session was revoked and goes back to sign-in.
  - So does any REST 401, through `onUnauthorized` in client.ts.
  - After two failed opens, the client checks `/api/me` in case the cookie was revoked.

## Store API

### Reading

```ts
import { useApp } from '../state/store'                        // any slice: useApp((s) => s.data.hasMore)
import { useSession, useNeeds, useUI } from '../state/selectors'

const working = useSession((s) => s.status === 'working')      // picks are compared shallowly
const sheet = useUI((u) => u.sheet)
```

| Hook | Returns |
|---|---|
| `useSession(select?)` | the session (View); with a selector, just that part (shallow compare) |
| `useStatus`, `useWorking`, `useControl`, `useNeeds`, `useSurface`, `useShowBrowser` | session pieces |
| `useFeed`, `useHasMore`, `useLoaded`, `useEntry(id)` | the feed |
| `useReport(id)`, `useNotes`, `useUnread` | reports and notifications |
| `useRun()` | the latest run event for the current run (phase, headline, tool, step), for RunCard |
| `useChanged(what)` | a counter that goes up on each `changed` event (`memory`, `schedules`, `logins`, `files`, `me`, ...); put it in an effect's deps to refetch. `reports` goes up on each `report` event, since the server announces reports that way |
| `useBoot`, `useSetup`, `useUser`, `useConfig`, `useConnection` | app state |
| `useUI(select)` | UI state: `menu`, `sheet`, `reflect`, `reflectTab`, `voice`, `toasts` |
| `useExchange()` | the entries of the current exchange (state/exchange.ts) |
| `useLive(select)`, `useHeard()` | the hot-path stores (state/live.ts) |

For a new store-shaped thing, use `createStore` and `useSelector(store, select, eq)` from store.ts, as live.ts does.

### Changing things (state/actions.ts)

Components call actions, never `api` plus `setState`. Every user action that reaches Alexa is a visible message: buttons call `send`/`answer`, never a hidden turn.

| Action | What it does |
|---|---|
| `send(text, attachments, viaVoice, via?)` | posts the message with a fresh `clientId`, upserts the entry the server returns, reads replies aloud after voice |
| `answer(text)` | `send` for a quick reply or a typed answer |
| `reply(text, tap?)` | a block's reply button: a visible message that says which card was tapped (`via: 'button'`, `tap`) |
| `takeOver`, `handBack`, `stop` | the browser hand-off and Stop |
| `show(surface)`, `openReport(id)`, `closePanel()`, `openRef(ref)`, `home()` | what the screen shows (optimistic, then PUT /api/session/surface). `home()` also puts away an exchange that is only waiting on a question |
| `openReflect(tab?)`, `closeReflect()`, `setReflectTab(tab)` | Reflect, as a history entry so Back closes it |
| `toggleMenu`, `closeMenu`, `openSheet(name)`, `closeSheet()` | the account menu and sheets |
| `setVoice(on)`, `toast(title, body?)` | voice mode and toasts |
| `loadMore()`, `loadReport(id)`, `refreshNotes()` | fetches into the store |
| `compact()`, `resetConversation()`, `setUser(u)`, `signOut()` | settings |

Low level, for the integrator only:

- `dispatch(action)` sends a reducer Action.
- `setUI(patch)` and `setApp(patch)` change app state.
- `onServerEvent(fn)` subscribes to raw events (spoken.ts uses it).
- `resync()` forces a snapshot.

Outside actions.ts:

- **The composer's outbox** (composer/outbox.ts) sends what the composer sends: `submit(msg)` gives each message a `clientId`, and the echo carries it back for dedupe. ⌘↵ sends with `interrupt: true` (POST /api/interrupt). The server's `queued` count marks messages Alexa hasn't read yet.
- **Prefill** (composer/focus.ts): `prefillComposer(text)` puts text in the composer, focused, with the caret at the end, and sends nothing. features/home/prefill.ts is the home board's way in. It returns false when no composer is mounted.
- **Try again and Repair** are the FailureCard's (POST /api/retry, /api/repair). Each makes a visible message on the server.

## Registries

| Registry | File | Key to component | Adding one |
|---|---|---|---|
| Entries | features/conversation/registry.ts | `entry.kind` to `{Component, announce?}`; unknown kinds show their text | a file in `entries/` plus one line |
| Panels | panels/registry.ts | `surface.kind` to panel (`browser`, `report`, `entry`); gets `{id}` | a folder in `panels/` plus one line |
| Sheets | sheets/registry.ts | `SheetName` to Settings (at its section) or the Notifications sheet; gets `{open, onClose}` | a section: a pane in `sheets/`, its `SettingsSection` (store.ts), a line in `Settings.tsx` and its entries in `settingsIndex.ts` |

The entries' props are `{entry, mode: 'now' | 'history', compact?}`. Use `mode === 'history'` for Reflect, where nothing can be answered any more.

Steps aren't an entry kind. The Log groups runs of step entries into one `StepsBlock`. `renderEntry(entry, mode)` renders any entry outside the Log, for example in a gallery or a panel.

## Mount points

Every mount point is filled and mounted where it belongs. To change one, work in place: you don't need to touch the parent.

| Mount | Mounted in | Props | Package |
|---|---|---|---|
| `RunCard` | Stage: head of the exchange column (`Log head`) | reads the store | WP-06 progress |
| `StepsBlock`, `WorkingLine` | Log: each run of steps; under the exchange while working | `{steps, live}`; none | WP-06 progress |
| `PlanEntry` | entry registry, kind `plan` | `EntryProps` | WP-06 progress |
| `BlockList` | SayEntry, AskEntry and AskCard (inline), ReportPanel (report), EntryPanel (panel) | `{blocks?, placement, frozen?, entryId?}` | WP-04 blocks |
| `Sources` | SayEntry | `{sources?}` | WP-04 blocks |
| `EntryPanel` | panel registry, surface `{kind:'entry', id}` | `{id}` | WP-04 blocks |
| `HomeBoard` | Stage, when the exchange is empty (`Log home`) | none | WP-08 stage+home |
| `Suggestions` | Log, under the final say of the current exchange | `{suggestions, onPick}` | WP-08 stage+home |
| `Caption` | Stage, under the panel | reads the store | WP-07 stage+home |
| `NeedsDock` | Stage, above the composer | reads the store | WP-10 ask+composer |
| `AskCard` | AskEntry, while the ask is open | `{entry}` | WP-10 ask+composer |
| `ConnectionPill` | Header | reads `useConnection()` | WP-10 ask+composer |
| `Palette` | App | none | WP-12 chrome |

Panels mount once, in Stage's panel slot through `PanelHost` (FE-17). `usePanel()` keeps a panel briefly after it closes so it can animate out (`closing`).

## Styles

`main.tsx` imports `styles/tokens.css`, then `styles/base.css`, which imports `ui/ui.css`. Each feature then imports its own stylesheet from its component (blocks.css, progress.css, layout.css, home.css, report.css, composer.css, ask.css, browser.css, sheets.css, reflect.css, conversation.css), so feature rules come after the global ones. The old styles.css and legacy.css are gone.

- **tokens.css and base.css** belong to the design system (WP-01).
- **Colours live only in tokens.css.** Feature stylesheets use the tokens. The one literal colour outside it is the `theme-color` meta in index.html, for the first paint; after that lib/theme.ts copies the page's computed background into it.

Theme is `data-theme="light|dark"` on `<html>`, or nothing for the system theme (lib/theme.ts).

## The dev gallery

`npm run dev`, then open `http://localhost:5173/?gallery` (or `?gallery=<name>`). It's for development only and is left out of the production build. There's no sign-in and no socket.

The toolbar picks:

- the page
- the theme: system, light or dark
- the width: 380, 680 or 1280, side by side or one at a time

Each width is a real iframe, so media queries apply as they would on a device. The URL keeps your choices (`&theme=dark&w=380`).

To add a page, create a `gallery.tsx` anywhere under `src/` that default-exports a `GalleryPage`:

```tsx
import { Section } from '../../dev/Section'
import type { GalleryPage } from '../../dev/types'

const page: GalleryPage = {
  name: 'progress',              // ?gallery=progress
  title: 'Progress',
  render: () => (
    <Section title="Working, three steps in">
      <RunCard />
    </Section>
  ),
}
export default page
```

It's picked up automatically through `import.meta.glob`, so no registration is needed. features/conversation/gallery.tsx shows how to seed the store for components that read it. Fixtures live next to the feature:

- features/conversation/fixtures.ts
- features/blocks/fixtures.ts, which has every Appendix A.6 fixture plus at least one of each block type

## Checks

```
cd web
npx tsc --noEmit
npx vitest run        # or: npm test
npm run build         # writes web/dist, embedded by the Go binary
```

Specs live next to the code (`*.test.ts`) and run in vitest's node environment. The reducer, the exchange and the connection are pure or take their environment as arguments, so they test without a DOM.

## Ownership (next stage)

Edit only what you own. For a change in someone else's file, write a handoff with the exact change.

| Owner | Files |
|---|---|
| design system (WP-01) | styles/tokens.css, styles/base.css, ui/** |
| blocks (WP-04) | features/blocks/**, lib/format.ts, panels/entry/** |
| progress (WP-06) | features/progress/**, features/conversation/entries/PlanEntry.tsx |
| stage+home (WP-07, WP-08) | app/Stage.tsx, app/layout.css, panels/PanelHost.tsx, panels/PanelSheet.tsx (new, the phone sheet for a panel), state/exchange.ts, features/home/**, features/caption/**, features/conversation/{Log.tsx, Suggestions.tsx, registry.ts, linkify.tsx, fixtures.ts, gallery.tsx}, entries/{UserEntry, SayEntry, NoteEntry, FallbackEntry}.tsx |
| ask+composer (WP-10) | features/ask/** (including LoginForm.tsx), entries/{AskEntry, TakeoverEntry, LoginEntry, ErrorEntry}.tsx, composer/** |
| report (WP-09) | panels/report/** (including html.ts), entries/ReportEntry.tsx, public/alexa-base.css (new) |
| browser (WP-11) | panels/browser/** |
| chrome (WP-12) | app/Header.tsx, app/OverlayHost.tsx (including Toasts), sheets/** (including registry.ts), reflect/**, auth/**, features/palette/** |
| integrator | app/App.tsx, state/{store, reducer, selectors, connection, actions, live}.ts, api/*.ts, panels/registry.ts, main.tsx, package.json |
| shared | dev/** (the gallery shell; each package adds its own `gallery.tsx` next to its feature), lib/{theme, url, useExit}.ts (additions only) |

Notes for owners:

- **Contract changes go to the integrator as a handoff.** This covers new fields, events, actions and selectors. A field the backend sends but the types lack is an additive optional field in api/*.ts.
- **Fixed since the last stage:** the CSP meta in panels/report/html.ts goes in through the parser (FE-05); a report that fails to load offers Try again (FE-34); the caption shows only in the phone sheet; upload errors aren't toasted as "Voice" any more.
- **Contract tests.** internal/api/contract_test.go, wire_contract_test.go and internal/agent/contract_test.go read the types in src/api (and the live socket's types in panels/browser/source.ts) and fail when the Go side drifts. Change both sides together.
