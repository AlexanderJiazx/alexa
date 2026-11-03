import type { Block } from '../../api/blocks'
import type { Entry, Report } from '../../api/types'
import { fixtures as blockFixtures } from '../../features/blocks/fixtures'

// Report fixtures: the Hacker News report exactly as a real run wrote it,
// a report that uses every kind of extra and some blocks, a table-heavy
// one, one built from blocks alone, and an HTML page on the theme bridge.
// The gallery shows them; tests read them too.

const min = 60e3
const now = Date.now()
const block = (name: string): Block => blockFixtures.find((f) => f.name.startsWith(name))!.block

// The Hacker News report, verbatim from a real run (data: {}): headings,
// "**Label:** value" bullets and bare domains instead of links.
export const hnContent = `Snapshot taken from https://news.ycombinator.com front page. Points/comments move fast – these are as-seen when opened.

## 1. Binance-hacker-1029 – $5.6B stolen from Binance
- **Link:** arkm.com (Arkham Intel entity page \`21a94755...\`)
- **Stats:** ~950 points, 557 comments, 7h ago by Tiberium
- **What:** Arkham-labeled on-chain entity allegedly tied to $5.6B taken from Binance. Thread is forensics / was-it-really-Binance, how Arkham labels work, and scale of the theft.
- **Why hot:** Highest comment count in top 5 – crypto-crime + data sleuthing always explodes.

## 2. We're releasing the weights of Mistral Large 4
- **Link:** mistral.ai/news/mistral-large-4-oss/
- **Stats:** ~1,277 points, 357 comments, 14h ago by kuberwastaken
- **What:** Mistral Large 4 (Le Chonk) – 1T-parameter natively multimodal MoE, open-weights. Pitched as best open-weight for coding + defensive cyber, with 3 reasoning modes (Low/Medium/High), $0.70/M in / $2.10/M out, download on Hugging Face, deploy with vLLM/SGLang.
- **Why hot:** Biggest points in top 5. HN debating benchmarks (FrontierCode 19.7%, Terminal-Bench 41%, Coding Agent Index 48% with 47% fewer tokens), pricing yo-yo, and sovereignty / self-host angle.

## 3. Cuis: The Modern Smalltalk-80
- **Link:** cuis.st
- **Stats:** ~34 points, 8 comments, 2h ago by theanonymousone
- **What:** Cuis – free, simple/powerful, portable Smalltalk-80 environment. Vector graphics, TrueType, Unicode, demo videos, 7.8 release in June, Smalltalks 2026 meetup news.
- **Why quiet:** Niche retro-language love – low points/comments, but classic HN Smalltalk crowd.

## 4. Astroterm: A Planetarium for Your Terminal
- **Link:** github.com/da-luce/astroterm
- **Stats:** ~36 points, 2 comments, 2h ago by nvmdbljstm
- **What:** C terminal star map – real-time stars, planets, constellations, moon phases in ASCII/Unicode/Braille. Set date/time/lat-long or \`--city Singapore\`, 24fps animation. 2.1k stars, 59 forks, MIT, packaged for Arch/Fedora/brew/Nix/Guix + binaries.
- **Why quiet:** Just posted – fun weekend-hack tool, hasn't had time to accumulate votes.

## 5. We ran AI agent swarms in Minecraft and Mindustry
- **Link:** labs.ycombinator.com/projects/games/
- **Stats:** ~222 points, 70 comments, 7h ago by sandslash (YC Labs + Olam Labs)
- **What:** Multi-agent alignment experiments:
  - *Civ50:* 50 GPT-6.1 Sol agents, 3h Minecraft survival, goal “Build thriving civilization” – 5,900 DMs, deforested spawn in 90s, Ada became leader, ration ledger, 22 of 34 deaths night 1, then risk-averse huddling.
  - *Mindustry + Bed Wars* sections, open-sourced \`yc-software/minecraft-agent-harness\`, Twitch 25-agent colony promo.
- **Why mid:** Solid discussion – emergent hierarchy, survival economics, harness design.

### Quick compare
- **By points:** Mistral Large 4 (~1277) > Binance-hacker (~950) > AI swarms (222) > Astroterm (36) > Cuis (34)
- **By comments:** Binance-hacker (557) > Mistral (357) > AI swarms (70) > Cuis (8) > Astroterm (2)
- **Split:** Top 2 are big-industry (crypto-heist + frontier open model); bottom 3 are hacker-maker (Smalltalk, terminal astronomy, agent-games).
`

export const hn: Report = {
  id: 'r-hn',
  title: 'Hacker News Top 5 – Points, Comments & What They Are',
  format: 'markdown',
  content: hnContent,
  data: {},
  createdAt: now - 3 * min,
  updatedAt: now - 3 * min,
}

// A weekend away: key figures, prose between blocks, dates, a file and
// sources.
export const trip: Report = {
  id: 'r-trip',
  title: 'A weekend in Lisbon, November 14 to 16',
  format: 'markdown',
  content: `Flights, a hotel in Alfama and a loose plan for two days. Everything below is held, not booked: tell me which to keep and I’ll confirm them.

## Getting there
TAP’s direct flight leaves Friday morning and is the cheapest of the four I checked. The 08:10 lands at 10:55, so you’ll have the afternoon.

## Where to stay
**Memmo Alfama** is the pick: a small hotel with a terrace over the river, a ten-minute walk from the tram. The [Lisbon tourism board](https://www.visitlisboa.com/en) lists it among the area’s best, and reviews on https://www.tripadvisor.com agree.

> Breakfast is included, and late check-out is free on Sundays if you ask the day before.
`,
  data: {
    items: [
      { label: 'Flights, return', detail: '$412' },
      { label: 'Hotel, 2 nights', detail: '€386' },
      { label: 'Walk to the tram', detail: '10 min' },
    ],
    events: [
      { title: 'TAP 1355 New York to Lisbon', start: '2026-11-14T08:10:00-05:00', end: '2026-11-14T20:55:00Z', location: 'JFK Terminal 1' },
      { title: 'Dinner at Taberna da Rua das Flores', start: '2026-11-14T20:00:00Z', location: 'Rua das Flores 103' },
      { title: 'Day trip to Sintra', start: '2026-11-15', allDay: true },
    ],
    files: ['reports/lisbon-weekend.pdf'],
    sources: [
      { title: 'TAP Air Portugal: flights to Lisbon', url: 'https://www.flytap.com/en-us/flights-to-lisbon' },
      { title: 'Memmo Alfama Design Hotel', url: 'https://www.memmohotels.com/alfama' },
      { title: 'Visit Lisboa: Alfama', url: 'https://www.visitlisboa.com/en/places/alfama' },
      { title: '', url: 'https://www.rtings.com/' },
    ],
    blocks: [
      block('stats'),
      { type: 'markdown', text: '## The plan\nSaturday is for Sintra; Sunday is slow, with the market and a long lunch.' },
      {
        type: 'timeline',
        days: [
          {
            date: '2026-11-15',
            items: [
              { time: '09:40', title: 'Train from Rossio to Sintra', kind: 'event', place: 'Rossio station' },
              { time: '13:00', title: 'Lunch at Tascantiga', kind: 'meal', place: 'Rua Marechal Saldanha 22' },
            ],
          },
        ],
      },
      block('callout: tip'),
    ],
    takeaway: 'TAP’s direct flight and two nights at Memmo Alfama come to about $830.',
  },
  createdAt: now - 26 * min,
  updatedAt: now - 26 * min,
}

// Tables, code, quotes, an ordered list and a task list.
export const tables: Report = {
  id: 'r-tables',
  title: 'Noise-cancelling headphones under $300',
  format: 'markdown',
  content: `I compared the five most recommended pairs on price, battery and how well they cancel noise on a plane.

## Prices today

| Model | Best price | Store | Battery | Weight |
| --- | --- | --- | --- | --- |
| Sony WH-1000XM6 | $279 | Best Buy | 30 h | 254 g |
| Bose QuietComfort Ultra | $299 | Amazon | 24 h | 250 g |
| Sennheiser Momentum 4 | $249.95 | B&H | 60 h | 293 g |
| Apple AirPods Max | $449 | Apple | 20 h | 385 g |
| Anker Soundcore Space One | $79.99 | Amazon | 40 h | 265 g |

## How I ranked them
1. Noise cancelling on a plane, from RTINGS’ measurements.
2. Price at a store that delivers by Friday.
3. Battery, then comfort.

### Next steps
- [x] Check delivery to your address
- [ ] Order the Sony from Best Buy
- [ ] Return the old pair by **November 20**

The model number is \`WH1000XM6/B\` if you want to check it yourself:

\`\`\`
Sony WH-1000XM6  black  $279.00
\`\`\`
`,
  data: { sources: [{ title: 'RTINGS: best noise cancelling headphones', url: 'https://www.rtings.com/headphones/reviews/best/by-feature/noise-cancelling' }] },
  createdAt: now - 3 * 3600e3,
  updatedAt: now - 3 * 3600e3,
}

// A report made only of blocks, with the takeaway as its lede.
export const blocksOnly: Report = {
  id: 'r-blocks',
  title: 'Three headphones for a long flight',
  format: 'markdown',
  data: {
    blocks: [block('cards: three products'), block('compare'), { type: 'markdown', text: 'Prices are from this morning, at stores that deliver by Friday.' }, block('table: prices by store')],
    takeaway: 'Get the Sony WH-1000XM6: the quietest on a plane, the longest battery, and $20 under the Bose.',
    preview: { kinds: '3 products, comparison, table' },
  },
  createdAt: now - 26 * 3600e3,
  updatedAt: now - 26 * 3600e3,
}

// An HTML page written for the bridge: its colours are the app's tokens,
// so it follows light and dark; its buttons send the user's choice back.
// It opens with a <header> and no <head>, the shape FE-05 got wrong.
export const htmlContent = `<header class="top">
  <p class="label">Fares · JFK to LIS</p>
  <h1>The cheapest week to fly is Nov 10</h1>
</header>
<style>
  .top { margin-bottom: var(--s5); }
  .bars { display: grid; gap: var(--s2); margin: var(--s5) 0; }
  .bar { display: grid; grid-template-columns: 72px 1fr 56px; align-items: center; gap: var(--s3); font: 400 var(--t-small) var(--font); }
  .bar i { display: block; height: 12px; border-radius: var(--r-full); background: var(--series-2); }
  .bar.best i { background: var(--series-1); }
  .bar b { text-align: right; font-variant-numeric: tabular-nums; }
  .choices { display: flex; flex-wrap: wrap; gap: var(--s2); margin-top: var(--s5); }
  #status { min-height: 18px; margin-top: var(--s3); color: var(--text-3); font: 400 var(--t-small) var(--font); }
</style>
<p class="muted">Return fares for one adult, economy, from the last 21 days of searches.</p>
<div class="bars" role="list">
  <div class="bar best" role="listitem"><span>Nov 10</span><i style="width: 58%"></i><b>$412</b></div>
  <div class="bar" role="listitem"><span>Nov 17</span><i style="width: 71%"></i><b>$498</b></div>
  <div class="bar" role="listitem"><span>Nov 24</span><i style="width: 92%"></i><b>$644</b></div>
  <div class="bar" role="listitem"><span>Dec 1</span><i style="width: 66%"></i><b>$463</b></div>
</div>
<div class="grid">
  <div class="card"><div class="label">Lowest</div><div class="stat">$412</div><div class="subtle">TAP, direct</div></div>
  <div class="card"><div class="label">Typical</div><div class="stat">$520</div><div class="subtle">one stop</div></div>
</div>
<div class="choices">
  <button class="primary" data-send="Book the Nov 10 TAP flight for $412.">Book Nov 10</button>
  <button data-send="Keep watching the fares and tell me if Nov 17 drops under $450.">Keep watching</button>
</div>
<p id="status" aria-live="polite"></p>
<script>
  var note = document.getElementById('status');
  document.querySelectorAll('[data-send]').forEach(function (b) {
    b.addEventListener('click', function () {
      alexa.send(b.dataset.send).then(function (r) { note.textContent = r.ok ? 'Sent.' : r.error; });
    });
  });
  // A send without a tap is refused.
  alexa.send('Sent on load').then(function (r) { note.textContent = 'On load: ' + (r.ok ? 'sent' : r.error); });
</script>
`

export const page: Report = {
  id: 'r-html',
  title: 'Fares from New York to Lisbon, by week',
  format: 'html',
  content: htmlContent,
  data: {},
  createdAt: now - 8 * min,
  updatedAt: now - 8 * min,
}

export const reports = [hn, trip, tables, blocksOnly, page]

// ---------- the cards in the exchange ----------

// A photo-like thumbnail, drawn: a sky, a river and a roof line.
const photo =
  'data:image/svg+xml,' +
  encodeURIComponent(
    "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 96 96'><rect width='96' height='96' fill='lightsteelblue'/><rect y='58' width='96' height='38' fill='steelblue'/><path d='M0 62 L18 40 L30 52 L46 30 L62 50 L74 42 L96 60 L96 64 L0 64 Z' fill='sienna'/><circle cx='74' cy='22' r='9' fill='khaki'/></svg>",
  )

let next = 5000
function card(r: Report | null, title: string, data: Record<string, unknown> = {}): Entry {
  const at = r?.updatedAt ?? now - min
  return { id: next++, kind: 'report', text: title, data: { id: r?.id, ...data }, createdAt: at, updatedAt: at }
}

// As an older server sends them: the id only, and the report not loaded.
export const cardBare = card(null, 'Noise-cancelling headphones under $300', { id: 'r-missing' })
export const cardHn = card(hn, hn.title)
export const cardTrip = card(trip, trip.title, { takeaway: trip.data?.takeaway, preview: { stat: { label: 'Flights', value: '$412', delta: '−$38' }, kinds: 'itinerary' } })
export const cardThumbs = card(null, 'Three hotels in Alfama with a river view', {
  id: 'r-thumbs',
  takeaway: 'Memmo Alfama is the pick for the terrace; Santiago de Alfama if you want a bigger room.',
  preview: { thumbs: [photo], kinds: '3 stays, comparison' },
})
export const cardTables = card(tables, tables.title, { updated: true })
export const cardBlocks = card(blocksOnly, blocksOnly.title, { takeaway: blocksOnly.data?.takeaway, preview: blocksOnly.data?.preview })
export const cardPage = card(page, page.title)
