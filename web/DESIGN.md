# Alexa’s design system (v3, “Ink”)

Alexa looks like warm paper with ink on it: calm, monochrome and flat. There
is no accent colour and no elevation. This file is the short version of the rules. The source of
truth is code:

- `src/styles/tokens.css`: every colour, size, space, radius and timing.
- `src/styles/base.css`: element defaults, focus, selection, scrollbars, reduced motion, utilities and the shared keyframes.
- `src/ui/`: the primitives (one per concern) and `ui.css`, their styles.
- `src/ui/gallery.tsx`: every token and primitive on one page. Run the dev server and open `/?gallery=ui`. It shows both themes side by side and frames at 380, 680 and 1280px.

`src/styles/tokens.test.ts` checks the rules that can be checked: the
`light-dark()` fallback is in step, every colour but status is a grey, text-2
and text-3 reach 4.5:1 on every surface, nothing has a shadow, and no
stylesheet or component writes a colour literal.

## Principles

1. **Flat, not boxed.** Content sits on the page. A card means “this is one
   thing you can pick up” (a report, an attachment), not “this is a section”.
   Sections are divided by a hairline and space. Never put a card in a card;
   if you must, the inner one is `flat`.
2. **One primitive per concern.** There is one button, one dot, one menu, one
   sheet, one toast. If a feature needs a variant, add it to the primitive (and
   the gallery), not a look-alike in the feature’s CSS.
3. **Ink, no accent.** Everything is ink on paper, in four text tiers. What
   needs attention gets weight, an ink fill or space, never a colour. The
   status colours (`--ok`, `--warn`, `--bad`) only say a state: done,
   careful, failed. Don’t add an accent or a new colour without asking.
4. **The dot is presence.** `Mark` is Alexa everywhere: still when idle,
   breathing with a fine ink orbit while working, a slow breath while you’re
   in control, and an ink ring pulsing out when Alexa needs you. The wave bars
   belong to voice only.
5. **Numbers are tabular.** Timers, counts, sizes, tokens, prices and table
   columns use `font-variant-numeric: tabular-nums` (`.num`), so nothing
   jitters as it ticks.
6. **Motion has a job.** Things enter with `--ease-out` in `--d-2` to
   `--d-3` and leave in about 0.6× that, so exits are quicker than entrances.
   Things that arrive together stagger by `--stagger` (60ms) each, for six
   steps at most, so nobody waits on a long list (sign-in: headline, line,
   form). Controls scale to `.97` on press, large cards to `.985`. Panels and sheets settle with
   `--spring`. Under reduced motion, nothing moves, but loading still shows,
   as a fade.
7. **Seven type sizes, a 4px grid, radii by nesting.** Pick from the scale;
   never invent a size. An inner radius is the outer radius minus the padding.
8. **Flat.** There are no shadows: not under cards, not inside fields, not
   around menus. Things separate by a surface step (`--bg` to `--surface`)
   and a 1px line. Floating things (menus, sheets, dialogs, toasts) are the
   same surface with a frame, over the scrim when they’re modal. A control
   is a surface with a `--line-strong` frame; pointing at it darkens the
   frame to `--focus-line`, focusing a field makes it ink, pressing scales
   it to .97. Every screen uses these same few moves.

## Tokens

Use the token, never the value. HTML reports also get the names from
earlier versions as aliases (`public/alexa-base.css`); the app doesn’t use
them.

### Colour

Every colour is a `light-dark()` pair, so it follows the system. Settings can
pin a theme with `data-theme` on `<html>`. Browsers without `light-dark()`
get the same values from the `@supports` block at the end of `tokens.css`.
When you add or change a colour, change it in both places; the test fails
until you do.

| Token | Use |
| --- | --- |
| `--bg` | The page: warm paper. |
| `--surface` | Everything on the page: cards, bubbles, the panel, the composer, menus, sheets, dialogs, toasts, fields and buttons. |
| `--tile` | Small neutral fills: pills, image tiles, code, the segmented track, loading placeholders. |
| `--line` | Hairlines between things; the frame of a card or a floating surface. |
| `--line-strong` | The frame of a control. |
| `--focus-line` | The frame of a control you’re pointing at, and the composer while you type. |
| `--ink`, `--ink-2` | Text and primary fills; long reading text. |
| `--text-2` | Secondary text. At least 4.5:1 everywhere. |
| `--text-3` | Metadata and help. At least 4.5:1 everywhere. |
| `--text-4` | Placeholders, decoration and a switch’s frame. Never for text that must be read. |
| `--ok`, `--warn`, `--bad` (+ `-bg`) | Status only: a foreground for text and icons, with a quiet background. |
| `--hover`, `--press`, `--select`, `--scrim` | State washes and the modal backdrop: `--hover` under the pointer, `--press` while pressed, `--select` on the chosen row or section (in ink). Hover rules sit in `@media (hover: hover)`, so a tap on a phone leaves nothing behind (tokens.test.ts checks). |
| `--canvas`, `--canvas-text`, `--pointer` | A web page (live browser, HTML reports) is white in both themes; Alexa’s pointer is ink on it. |

### Type

Use one of the seven sizes: `font: 500 var(--t-small) var(--font)`, with the
matching `--tr-*` tracking for body and up.

| Token | Size | Use |
| --- | --- | --- |
| `--t-micro` | 12/16 | Captions, table heads, timestamps. |
| `--t-small` | 13/18 | Metadata, help, steps. |
| `--t-body` | 15/22 | UI text, messages. |
| `--t-read` | 17/26 (16 on phones) | Report prose. |
| `--t-title` | 20/28 | Section titles, a report’s lede. |
| `--t-head` | 28/34 | Page titles (Reflect). |
| `--t-display` | 44/48 (36 on phones) | The greeting, report titles and sign-in. Instrument Serif. |

`--font` is Instrument Sans (400, 500, 600). `--font-display` is Instrument
Serif (regular and italic), only at head and display sizes. `--font-mono` is
the system monospace.

### Space, radii, layers

- Space is `--s1` 4px, `--s2` 8, `--s3` 12, `--s4` 16, `--s5` 20, `--s6` 24, `--s8` 32, `--s10` 40 and `--s14` 56.
- Radii are `--r-xs` 6px, `--r-sm` 10 (controls), `--r-md` 14 (cards, fields on sign-in), `--r-lg` 20 (panels, sheets), `--r-xl` 28 and `--r-full`.
- Layout is `--header-h` (52px, 48 on phones), `--gutter` 16px and `--measure` 640px (reading width).
- Stacking, from bottom to top, is `--z-composer`, `--z-header`, `--z-page`, `--z-scrim`, `--z-menu`, `--z-sheet` and `--z-toast`.

### Focus

- `--focus` is ink. `base.css` gives every `:focus-visible` a 2px outline, offset 2px.
- A field shows focus by its frame turning ink; the composer’s frame turns `--focus-line`. Nothing glows.

### Motion

- Durations are `--d-1` 120ms (state changes), `--d-2` 200ms (small enters), `--d-3` 320ms (panels, sheets) and `--d-4` 480ms (large moves). `--stagger` is 60ms.
- The curves are `--ease-out` (enter), `--ease-in-out` (move and morph) and `--spring` (settle).
- The shared keyframes are in `base.css`: `pIn`/`pOut`, `pRise`/`pFall`, `pPop`/`pUnpop`, `pFromRight`/`pToRight`, `pFromBelow`/`pToBelow`, `pBreath`, `pOrbit`, `pPulse`, `pShimmer` and `pRipple`.

## Primitives

Import from `src/ui/`. Each has a section in the gallery.

| Primitive | Use it for | Notes |
| --- | --- | --- |
| `Button` (`buttonClass`) | Every text button | `primary` is ink, one per view. `secondary` is the default. `ghost` has no frame. `danger` is for what can’t be undone. Sizes are `sm` 32, `md` 40 and `lg` 48. `loading` shows a spinner. Use `buttonClass()` on an `<a>` or `<label>`. |
| `IconButton` | A glyph-only button | 44px (`sm` 36), and it always has a `label`. text-2, ink under the pointer; `tone="muted"` (text-3) for the quiet actions on a list's rows, like delete. Don't recolour it in a feature. |
| `Pill` | A status you can’t press | Tones are `neutral`, `ink`, `ok`, `warn` and `bad`. `dot` adds a leading dot; `pulse` makes it breathe, for something happening now. |
| `Chip` | A suggestion, filter or attachment | `selected`, and `onRemove` adds a ×. |
| `Card` (`cardClass`) | One thing you can pick up | `interactive` darkens its frame on hover. `flat` is for a card inside a card (the page colour inside). |
| `Mark` | Alexa’s presence | States are `idle`, `working`, `waiting` and `needs`. Use `draw` on sign-in. |
| `Segmented`, `Tabs` | A few exclusive choices, or switching views | The arrow keys move the choice. `block` stretches it. |
| `Toggle` | On/off | Always next to a visible label. |
| `Field`, `Input`, `TextArea`, `Select` | Labelled controls | An `error` replaces `help`. |
| `Dialog`, `ConfirmDialog` | A small question | A native `<dialog>`: it traps focus, Escape closes it and focus goes back. In ConfirmDialog, Cancel takes focus first. Use it instead of `confirm()`. |
| `Settings` | Every setting, in one place | A large native `<dialog>` (960×720 at most) with search and the sections down the side, the section on the right. Full screen on phones, where the list comes first and a section opens with Back. New settings go in a section and in `settingsIndex.ts`, so search finds them. |
| `Sheet` | The notifications inbox | It comes from the right on wide screens and is a bottom sheet with a grabber on phones. |
| `Menu`, `MenuItem`, `MenuSeparator` | A popover of actions | The arrow keys, Home and End move between items. Escape and Tab close it and focus returns. |
| `Toast`, `ToastStack` | A brief note | Bottom centre, on the surface with a frame. A `bad` toast is an alert. |
| `Skeleton` | Loading when the shape is known | `lines` draws a paragraph. Prefer it to a spinner. |
| `EmptyState` | A list with nothing yet | A glyph, a title, one line and one action. |
| `Favicon` | A site’s icon | A letter tile while it loads or if the site has none. |
| `Timer`, `useNow` | Elapsed time | Tabular, one shared clock. |
| `LiveAnnouncer`, `announce()` | Telling screen readers what changed | Mount it once and call it from anywhere. |
| `Kbd` | A shortcut hint | Desktop only. |
| `icons.tsx` | Glyphs | `ToolGlyph kind=…` is the icon for a tool step. Glyphs are 1.5px strokes on a 24px grid, in `currentColor`. |

Shared CSS classes in `ui.css` for markup that isn’t a component yet:

- `.list` (`.t1`, `.t2`, `.rowlink`): divided rows in Settings, the inbox and the home board's Recent list.
- `.list-head`: the heading over a list (Home's sections, Settings' groups, Reflect's shelves, the palette's groups), 13px semibold text-2 in sentence case.
- `.intro` and `.help`: explanatory text.
- `.row-btns`: a row of buttons.
- `.sec-title` (in `sheets.css`).
- `.spinner`.
- `.shimmer`.
- `.unread-dot`.

## Where styles live

- Tokens and element defaults are in `src/styles/`. The primitives are in `src/ui/ui.css`.
- Each feature imports its own stylesheet next to it:
  - `app/layout.css`
  - `composer/composer.css`
  - `features/*/*.css`
  - `panels/*/*.css`
  - `reflect/reflect.css`
  - `sheets/sheets.css`
  - `auth/auth.css`
- A feature’s CSS may place and size primitives, but it doesn’t restyle them.

## Do and don’t

**Do:**

- use tokens for every colour, size, space, radius and duration.
- use the primitive, and add a variant to it when you need one.
- put a new primitive or variant in the gallery and check it in both themes at 380px.
- divide sections with a hairline and space.
- keep inline `style` for values that are truly dynamic (a measured height, an aspect ratio, a custom property). Use classes for everything else.
- use tabular numbers for anything that counts or ticks.
- write copy with curly apostrophes (’).

**Don’t:**

- write a hex, `rgb()` or `hsl()` value outside `tokens.css`. The test fails if you do.
- add a shadow, a glow, an accent colour or a gradient that fakes depth. The test fails on a shadow.
- put text that must be read in `--text-4`.
- nest cards, or give a feature its own look for a control the primitives already have.
- invent a font size, a spacing value or a radius.
- animate layout properties (`width`, `height`, `top`) when `transform` and `opacity` can do it. The voice bars are the one exception.
- use the wave bars for anything but voice.
