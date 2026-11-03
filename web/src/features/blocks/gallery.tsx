import type { ReactNode } from 'react'
import type { Block, Placement } from '../../api/blocks'
import type { Entry } from '../../api/types'
import { Section } from '../../dev/Section'
import type { GalleryPage } from '../../dev/types'
import { EntryPanel } from '../../panels/entry/EntryPanel'
import '../../panels/report/report.css'
import { store } from '../../state/store'
import { BlockList } from './BlockList'
import { LinkChip } from './LinkChip'
import { Sources } from './Sources'
import { fixtures, malformedFixtures, shapeFixtures, specFixtures, type Fixture } from './fixtures'
import './gallery.css'

// The blocks' gallery page (?gallery=blocks): every fixture where it would
// show (under what Alexa said, in the exchange's column), a few in the
// panel and a report, frozen in history, malformed ones falling back, and
// the sources row. Replies and Expand are stood in for, so nothing here
// reaches the server.

const noop = () => {}

// The frozen section's entries: a say with a reply, then what the user
// answered.
const saidId = 900001
// The entry the Expand panel shows.
const entryId = 900003
let seeded = false
function seed() {
  if (seeded) return
  seeded = true
  const said: Entry = { id: saidId, kind: 'say', text: 'This is the one I’d get.', data: { blocks: [specFixtures[0].block] }, createdAt: 0, updatedAt: 0 }
  const answered: Entry = { id: saidId + 1, kind: 'user', text: 'Let\'s go with the Sony WH-1000XM6 ($279).', createdAt: 0, updatedAt: 0 }
  const now = Date.now() - 4 * 60e3
  const expanded: Entry = { id: entryId, kind: 'say', text: 'Here’s how they stack up, and what the fares have done.', data: { blocks: [find('compare: four with prices'), find('table: 23 rows (a report)'), find('timeline: a weekend')] }, createdAt: now, updatedAt: now }
  store.set((s) => ({ ...s, data: { ...s.data, feed: [...s.data.feed.filter((e) => e.id < saidId), said, answered, expanded] } }))
}

function Said({ text, children }: { text?: string; children: ReactNode }) {
  return (
    <div className="log-inner">
      {text && (
        <div className="msg agent">
          <p>{text}</p>
        </div>
      )}
      {children}
    </div>
  )
}

const inline = (f: Fixture) => (
  <Section key={f.name} title={f.name}>
    <Said text={f.say}>
      <BlockList blocks={[f.block]} placement={f.block.type === 'markdown' ? 'report' : 'inline'} entryId={1} onReply={noop} onExpand={noop} />
    </Said>
    <details className="g-json">
      <summary>JSON</summary>
      <pre>{JSON.stringify(f.block, null, 2)}</pre>
    </details>
  </Section>
)

const find = (name: string) => [...fixtures, ...shapeFixtures].find((f) => f.name === name)!.block

function Placed({ title, placement, blocks }: { title: string; placement: Placement; blocks: Block[] }) {
  return (
    <Section title={title}>
      <div className={placement === 'report' ? 'g-blocks-report report' : 'g-blocks-panel'}>
        <BlockList blocks={blocks} placement={placement} onReply={noop} onExpand={noop} />
      </div>
    </Section>
  )
}

const page: GalleryPage = {
  name: 'blocks',
  title: 'Blocks',
  render() {
    seed()
    return (
      <div className="g-blocks">
        <Section title="Sources and link chips">
          <Said text="The Sony is the quietest on planes, by every test I found.">
            <Sources
              sources={[
                { title: 'Sony WH-1000XM6 review: the new king of quiet', url: 'https://www.theverge.com/reviews/sony-xm6' },
                { title: 'Best noise-cancelling headphones', url: 'https://www.nytimes.com/wirecutter/reviews/best-noise-cancelling-headphones/' },
                { title: 'Sony WH-1000XM6 headphones review', url: 'https://www.rtings.com/headphones/reviews/sony/wh-1000xm6' },
                { url: 'https://www.sony.com/electronics/headband-headphones/wh-1000xm6' },
                { title: 'r/headphones: XM6 vs QC Ultra', url: 'https://www.reddit.com/r/headphones/' },
              ]}
            />
          </Said>
          <div className="log-inner">
            <div className="msg agent">
              <p>
                It’s on <LinkChip url="https://www.bestbuy.com/site/sony-wh1000xm6/6571234.p" /> for $279, and <LinkChip url="https://www.amazon.com/" /> has it too.
              </p>
            </div>
          </div>
        </Section>
        {[...specFixtures, ...shapeFixtures.slice(0, 3)].map(inline)}
        <Section title="Frozen: a past exchange, answered">
          <Said text="This is the one I’d get.">
            <BlockList blocks={[specFixtures[0].block, find('cards: three products')]} placement="inline" frozen entryId={saidId} onReply={noop} onExpand={noop} />
          </Said>
        </Section>
        {fixtures
          .filter((f) => !specFixtures.includes(f) && !shapeFixtures.slice(0, 3).includes(f))
          .map(inline)}
        {malformedFixtures.map((f) => (
          <Section key={f.name} title={f.name}>
            <Said>
              <BlockList blocks={[f.block]} placement="inline" />
            </Said>
          </Section>
        ))}
        <Placed title="Panel: a comparison, a table and a carousel as a grid" placement="panel" blocks={[find('compare: four with prices'), find('table: 23 rows (a report)'), find('cards: six products (carousel)')]} />
        <Section title="Expand: the entry panel">
          <div className="g-blocks-entry">
            <EntryPanel id={String(entryId)} />
          </div>
        </Section>
        <Placed title="Report: stats, prose, cards and a timeline" placement="report" blocks={[find('stats: a month of spending'), find('markdown (reports only)'), find('cards: three products'), find('timeline: a weekend')]} />
      </div>
    )
  },
}

export default page
