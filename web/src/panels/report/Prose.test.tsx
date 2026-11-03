import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { mdImageSrc } from '../../features/blocks/parts/MdImage'
import { MarkdownBlock } from '../../features/blocks/markdown/Markdown'
import { hnContent } from './fixtures'
import { Prose } from './Prose'
import { bareAddress, isNumeric, ranking } from './rehypeReport'

const html = (md: string) => renderToStaticMarkup(<Prose text={md} />)

describe('Prose', () => {
  it('sets "**Label:** value" lists as definition rows', () => {
    const out = html('- **Stats:** ~950 points\n- **Why hot:** Most comments')
    expect(out).toContain('<dl class="defs"><div class="def"><dt>Stats</dt><dd>~950 points</dd></div><div class="def"><dt>Why hot</dt><dd>Most comments</dd></div></dl>')
  })

  it('keeps a list that isn’t all labels as bullets', () => {
    const out = html('- **Stats:** ~950 points\n- just a line')
    expect(out).toContain('<ul>')
    expect(out).not.toContain('<dl')
  })

  it('links a bare domain after a Link label, as a pill', () => {
    const out = html('- **Link:** mistral.ai/news/mistral-large-4-oss/\n- **Stats:** 1,277 points')
    expect(out).toContain('class="pill-link" href="https://mistral.ai/news/mistral-large-4-oss/"')
    expect(out).toContain('<span class="pl-host">mistral.ai</span><span class="pl-path">/news/mistral-large-4-oss</span>')
  })

  it('turns a link that shows its own address into a pill, and leaves named links alone', () => {
    const out = html('From https://news.ycombinator.com and [the board](https://www.visitlisboa.com/en).')
    expect(out).toContain('class="pill-link" href="https://news.ycombinator.com"')
    expect(out).toContain('<a href="https://www.visitlisboa.com/en" target="_blank" rel="noopener noreferrer">the board</a>')
  })

  it('hangs a section’s number in a span of its own', () => {
    expect(html('## 2. Mistral Large 4')).toContain('<h2 class="numbered"><span class="h-num">2</span>Mistral Large 4</h2>')
    expect(html('## Quick compare')).toContain('<h2>Quick compare</h2>')
  })

  it('keeps tables as tables in a scroller, with numeric columns marked', () => {
    const out = html('| Model | Price | Store |\n| --- | --- | --- |\n| Sony | $279 | Best Buy |\n| Bose | $299.00 | — |')
    expect(out).toMatch(/^<div class="prose"><div class="table-wrap"><table class="short">/)
    expect(out).toContain('<th class="num">Price</th>')
    expect(out).toContain('<td class="num">$279</td>')
    expect(out).not.toContain('<th class="num">Store</th>')
  })

  it('lets a table of long cells wrap', () => {
    const out = html('| Hotel | Why |\n| --- | --- |\n| Memmo | A small hotel with a terrace over the river and free late check-out |')
    expect(out).toContain('<div class="table-wrap"><table>')
  })

  it('makes a "#" heading a part, so the title stays the one h1', () => {
    expect(html('# Part one')).toContain('<h2 class="part">Part one</h2>')
  })

  it('draws a ranking row as bars, the leader first', () => {
    const out = html('- **By points:** Mistral Large 4 (~1277) > Binance-hacker (~950) > Cuis (34)\n- **Split:** Top 2 are big-industry.')
    expect(out).toContain('<dt>By points</dt><dd><ol class="ranks"><li class="lead"><span class="rk-name">Mistral Large 4</span><span class="rk-bar" aria-hidden="true" style="--v:100%"></span><span class="rk-v">~1,277</span></li>')
    expect(out).toContain('<li><span class="rk-name">Cuis</span><span class="rk-bar" aria-hidden="true" style="--v:3%"></span><span class="rk-v">34</span></li></ol></dd>')
    expect(out).toContain('<dt>Split</dt><dd>Top 2 are big-industry.</dd>')
  })

  it('renders the Hacker News report with every story as a numbered section of rows', () => {
    const out = html(hnContent)
    expect(out.match(/<h2 class="numbered">/g)?.length).toBe(5)
    expect(out.match(/<dl class="defs">/g)?.length).toBe(6)
    expect(out.match(/class="pill-link"/g)?.length).toBe(6)
    // Both rankings in its Quick compare are bars.
    expect(out.match(/<ol class="ranks">/g)?.length).toBe(2)
    // A nested list inside a row stays a list.
    expect(out).toMatch(/<dd>Multi-agent alignment experiments:\s*<ul>/)
  })
})

describe('images', () => {
  const token = '/api/img/0123456789abcdef0123456789abcdef'

  it('never load from a remote host, in a report or a markdown block', () => {
    const md = '![chart](https://tracker.example/p.gif?id=7) and ![](http://plain.example/x.png)'
    for (const out of [html(md), renderToStaticMarkup(<MarkdownBlock block={{ type: 'markdown', text: md }} />)]) {
      expect(out).not.toContain('<img')
      expect(out).not.toContain('tracker.example/p.gif')
      expect(out).toContain('class="md-img-off"')
      expect(out).toContain('<span class="md-img-host">tracker.example</span>')
    }
  })

  it('load the server’s proxied pictures and workspace images', () => {
    expect(html(`![A chart](${token})`)).toContain(`<img src="${token}" alt="A chart"`)
    expect(html('![Plan](plans/floor.png)')).toContain('<img src="/api/files/download?path=plans%2Ffloor.png&amp;inline=1"')
  })

  it('refuse anything else', () => {
    for (const s of ['https://a.example/x.png', '//a.example/x.png', '/api/files/download?path=x.png', '/api/img/nope', 'data:image/png;base64,AAAA', '../secret.png', 'notes.txt', 'javascript:alert(1)', ''])
      expect(mdImageSrc(s), s).toBe('')
    expect(mdImageSrc(token)).toBe(token)
    expect(mdImageSrc('out/chart.webp')).toBe('/api/files/download?path=out%2Fchart.webp&inline=1')
  })
})

describe('the rules', () => {
  it('knows a bare address', () => {
    expect(bareAddress('https://www.rtings.com/', 'rtings.com')).toBe(true)
    expect(bareAddress('https://mistral.ai', 'https://mistral.ai')).toBe(true)
    expect(bareAddress('https://mistral.ai', 'Mistral')).toBe(false)
    expect(bareAddress('javascript:alert(1)', 'javascript:alert(1)')).toBe(false)
  })

  it('knows a ranking, and a sentence that only looks like one', () => {
    expect(ranking('A (3) > B (2) > C (2)')?.map((r) => r.value)).toEqual([3, 2, 2])
    expect(ranking('Binance (hack) (1,950) > Mistral (357) > Cuis (8.5).')?.map((r) => [r.name, r.shown])).toEqual([
      ['Binance (hack)', '1,950'],
      ['Mistral', '357'],
      ['Cuis', '8.5'],
    ])
    // Two links, a link without a figure, or figures out of order.
    expect(ranking('A (3) > B (2)')).toBeNull()
    expect(ranking('A (3) > B > C (1)')).toBeNull()
    expect(ranking('A (1) > B (2) > C (3)')).toBeNull()
    expect(ranking('Price > battery > comfort')).toBeNull()
  })

  it('knows a number', () => {
    for (const s of ['$279', '1,277', '30 h', '48%', '~950', '€386', '−$38', '2.1k', '254 g', '21°C']) expect(isNumeric(s), s).toBe(true)
    for (const s of ['Best Buy', 'Sony WH-1000XM6', 'Nov 10']) expect(isNumeric(s), s).toBe(false)
  })
})
