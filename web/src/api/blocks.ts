// Generative UI blocks: the trusted, declarative catalog Alexa's say, ask and
// present_report tools can attach (SYNTHESIS Appendix A.2). The server
// validates and clamps every block (internal/agent/blocks.go mirrors these
// types), so the client renders them as given and never trusts colours or
// fonts from the model. Field names are snake_case, exactly as the model
// writes them.

export type Money = { amount: number; currency: string } // ISO 4217, 3 letters
export type Fact = { label: string; value: string } // label ≤24, value ≤60
export type Rating = { value: number; out_of?: number; count?: number } // out_of defaults to 5
export type Img = string // https URL (proxied via /api/img) or workspace path
export type Source = { title?: string; url: string } // title ≤80, http(s)
export type Action =
  | { do: 'reply'; label: string; text: string } // label ≤24, text ≤200
  | { do: 'open'; label?: string; url: string }
  | { do: 'report'; label?: string; id: string | number } // the server always sends the report's id as a string
// Dates: "2026-11-14", "2026-11-14T19:30" (user's zone) or RFC 3339.
type Base = { id?: string /* ≤32, [a-z0-9-] */; title?: string /* ≤60 */ }

export type Card = {
  title: string
  subtitle?: string
  url?: string
  image?: Img // title, subtitle ≤80
  price?: Money & { was?: number; per?: 'night' | 'person' | 'month' | 'total' }
  rating?: Rating
  badges?: string[] /* ≤3, ≤24 each */
  facts?: Fact[] /* ≤4 */
  note?: string /* ≤140 */
  pick?: boolean /* at most one per block */
  action?: Action
  // place
  address?: string
  hours?: string
  open_now?: boolean
  distance?: string
  phone?: string
  lat?: number
  lng?: number
  // stay
  check_in?: string
  check_out?: string
  // event
  start?: string
  end?: string
  venue?: string
  // flight
  segments?: { carrier: string; number?: string; from: string; to: string; depart: string; arrive: string }[] // ≤4
  duration?: string
  stops?: number
  cabin?: string
}

export type Block = Base &
  (
    | { type: 'cards'; kind?: 'product' | 'place' | 'stay' | 'flight' | 'event' | 'item'; items: Card[] } // 1-8
    | {
        type: 'compare'
        items: { name: string; price?: Money; pick?: boolean }[] // 2-5
        rows: { label: string; values: string[]; best?: number }[]
        verdict?: string
      } // rows ≤12, verdict ≤200
    | { type: 'table'; columns: string[]; align?: ('left' | 'right' | 'center')[]; rows: string[][] } // ≤6 cols, ≤40 rows
    | {
        type: 'stats'
        items: { label: string; value: string; delta?: string; trend?: 'up' | 'down' | 'flat'; good?: 'up' | 'down'; note?: string }[]
      } // 1-6
    | {
        type: 'chart'
        style: 'line' | 'bar'
        x: 'time' | 'category'
        currency?: string
        unit?: string
        series: { name: string; points: { x: string | number; y: number }[] }[] // ≤3 series, ≤60 points
        marks?: { x: string | number; label: string }[]
      } // ≤5
    | {
        type: 'timeline'
        days: {
          date: string
          items: { time?: string; end?: string; title: string; kind?: 'flight' | 'stay' | 'meal' | 'event' | 'travel' | 'task'; place?: string; note?: string }[]
        }[]
      }
    | { type: 'checklist'; items: { text: string; status: 'done' | 'failed' | 'todo' | 'skipped'; note?: string }[] } // ≤20
    | {
        type: 'receipt'
        status: 'draft' | 'pending' | 'confirmed' | 'cancelled' | 'failed'
        merchant?: string
        lines: { label: string; value: string }[]
        total: Money
        ref?: string
      } // lines ≤12
    | { type: 'links'; items: { title: string; url: string; note?: string }[] } // 1-12
    | { type: 'images'; items: { src: Img; caption: string; url?: string }[] } // 1-12, caption = alt
    | { type: 'files'; items: string[] } // workspace paths, ≤12
    | { type: 'callout'; tone: 'info' | 'tip' | 'warning' | 'success'; text: string } // text ≤280
    | { type: 'markdown'; text: string } // reports only
  )

export type BlockType = Block['type']

// BlockOf narrows Block to one type, for renderers: BlockOf<'cards'>.
export type BlockOf<T extends BlockType> = Extract<Block, { type: T }>

// Where a block list renders. Inline is the exchange column (380-720px),
// report the report panel's 640px measure, panel the Expand target.
export type Placement = 'inline' | 'report' | 'panel'

// The rich parts of an ask (Appendix A.4), stored on the ask entry's data
// and on Needs while it waits.
export type FormFieldType = 'text' | 'number' | 'date' | 'time' | 'datetime' | 'select' | 'multiselect' | 'toggle' | 'stepper'
export type FormField = {
  name: string // [a-z_]{1,24}
  label: string // ≤24
  type: FormFieldType
  value?: string | number | boolean | string[]
  required?: boolean
  min?: number
  max?: number
  step?: number
  options?: string[] // ≤8
  placeholder?: string // ≤40
}
export type AskForm = { submit?: string /* ≤24 */; fields: FormField[] /* ≤6 */ }

// How a visible user reply was made: a tapped block action, a submitted
// form, a multi-select, the report bridge (alexa.send) or voice.
export type Via = 'button' | 'form' | 'multi' | 'report'

// Tap says which block action a 'button' reply came from: the entry, the
// block's index in it and the item's index in the block.
export type Tap = { entryId: number; block: number; item: number }

// ReportPreview is what a report card shows in the exchange (WP-03): up to 3
// thumbnails, the first stat, or the kinds of blocks inside.
export type ReportPreview = {
  thumbs?: Img[] // ≤3
  stat?: { label: string; value: string; delta?: string }
  kinds?: string // e.g. "3 options, comparison, itinerary"
}
