import { describe, expect, it } from 'vitest'
import type { Entry } from '../../api/types'
import * as f from './fixtures'
import { foldGroups, groupSteps, hostFromText, kindFromText, reasonCase, sitesOf, stripHost, summarize, toStep, toSteps } from './group'

const texts = (entries: Entry[]) => groupSteps(toSteps(entries)).map((g) => g.text)

describe('groupSteps', () => {
  it('folds 14 steps on 3 sites into three lines', () => {
    const groups = groupSteps(toSteps(f.run14))
    expect(groups.map((g) => g.cat)).toEqual(['search', 'web', 'web'])
    expect(groups.map((g) => g.text)).toEqual([
      'Searched the web · 4 searches · 9 sources',
      'amazon.com: Opened Amazon, Searched Amazon for the Sony, +3 more',
      'bestbuy.com: Opened Best Buy, Checked store pickup, +1 more',
    ])
    expect(groups.map((g) => g.steps.length)).toEqual([5, 5, 4])
    expect(groups[1].host).toBe('amazon.com')
    // The failed click is its own line under its group, with its error.
    expect(groups[2].failed.map((s) => s.error)).toEqual(['A sign-up pop-up covered the button'])
    expect(groups[2].status).toBe('done')
  })

  it('merges searches across sites and keeps each source once', () => {
    const [search] = groupSteps(toSteps(f.run14))
    expect(search.sources).toHaveLength(9)
    expect(new Set(search.sources.map((s) => s.url)).size).toBe(9)
    expect(search.meta).toEqual(['4 searches', '9 sources'])
  })

  it('names one search by its query', () => {
    expect(texts(f.run14.slice(0, 1))).toEqual(['Searched for “best noise cancelling headphones 2026” · 4 sources'])
  })

  it('reads steps without step data from their words', () => {
    expect(texts(f.plainRun)).toEqual([
      'Searched the web · 2 searches',
      'Read rtings.com',
      'bestbuy.com: Opened, Compared prices',
      'You took over',
      'Checked delivery dates',
      'Saved the comparison to headphones.md',
      'Remembered that you prefer over-ear headphones',
    ])
    const groups = groupSteps(toSteps(f.plainRun))
    // The failed click happened on the page Alexa was on.
    expect(groups[4].host).toBe('bestbuy.com')
    expect(groups[4].failed.map((s) => s.label)).toEqual(['Clicking “Add to cart”'])
    expect(groups.map((g) => g.cat)).toEqual(['search', 'web', 'web', 'user', 'web', 'files', 'chip'])
  })

  it('counts file and shell work', () => {
    const files = toSteps(f.longRun).filter((s) => s.kind === 'file')
    const shell = { ...files[0], id: 1, kind: 'shell' as const, label: 'Ran the converter' }
    expect(groupSteps(files).map((g) => g.text)).toEqual(['Worked with files · 2 files'])
    expect(groupSteps([...files, shell]).map((g) => g.text)).toEqual(['Worked with files · 2 files, 1 command'])
    expect(groupSteps([shell, { ...shell, id: 2 }]).map((g) => g.text)).toEqual(['Ran 2 commands'])
  })

  it('makes memory and schedule steps chips', () => {
    const groups = groupSteps(toSteps(f.chipSteps))
    expect(groups).toHaveLength(1)
    expect(groups[0].cat).toBe('chip')
    expect(groups[0].items).toEqual(['Remembered you wear a medium', 'Scheduled a price check for Friday at 9am'])
  })

  it('marks a group running while its last step runs', () => {
    const groups = groupSteps(toSteps([...f.run14.slice(10, 12), f.runningStep]))
    expect(groups).toHaveLength(1)
    expect(groups[0].status).toBe('running')
  })
})

describe('foldGroups', () => {
  it('keeps the last four groups and the failed steps of the rest', () => {
    const groups = groupSteps(toSteps(f.longRun))
    expect(groups.map((g) => g.text)).toEqual([
      'Searched the web · 4 searches · 9 sources',
      'amazon.com: Opened Amazon, Searched Amazon for the Sony, +3 more',
      'walmart.com: Opened Walmart, Checked the price',
      'Worked with files · 2 files',
      'bestbuy.com: Opened Best Buy, Checked store pickup',
    ])
    const fold = foldGroups(groups)
    expect(fold.shown).toHaveLength(4)
    expect(fold.hidden).toBe(5)
    expect(fold.failed).toEqual([])
    const failing = groups.map((g, i) => (i === 0 ? { ...g, failed: g.steps.slice(0, 1) } : g))
    expect(foldGroups(failing).failed).toHaveLength(1)
  })

  it('leaves four or fewer groups alone', () => {
    const groups = groupSteps(toSteps(f.run14))
    expect(foldGroups(groups)).toEqual({ shown: groups, hidden: 0, failed: [] })
  })
})

describe('summarize', () => {
  it('reads a finished run in one line', () => {
    const s = summarize(toSteps(f.run14), { outcome: 'done', ms: (3 * 60 + 12) * 1e3 })
    expect(s.text).toBe('Done · 3m 12s · 14 steps on 3 sites')
    expect(s.sites).toEqual(['rtings.com', 'amazon.com', 'bestbuy.com'])
  })

  it('says why a run stopped', () => {
    expect(summarize(toSteps(f.failedSteps), { outcome: 'failed', reason: 'Couldn’t sign in to united.com' }).text).toBe('Stopped after 6 steps: couldn’t sign in to united.com')
    // Without a reason from the run, the last failed step's error is it.
    expect(summarize(toSteps(f.failedSteps), { outcome: 'failed' }).text).toBe('Stopped after 6 steps: couldn’t sign in to united.com')
    expect(summarize(toSteps(f.stoppedSteps), { outcome: 'stopped' }).text).toBe('Stopped after 6 steps')
  })

  it('leaves the user’s own steps out of the count', () => {
    const s = summarize(toSteps(f.plainRun), { outcome: 'done', ms: 95e3 })
    expect(s.text).toBe('Done · 1m 35s · 9 steps on 2 sites')
  })

  it('reads a run that isn’t over as its work and time', () => {
    expect(summarize(toSteps(f.run14.slice(5, 7)), { outcome: 'open' }).text).toBe('2 steps on 1 site · 8s')
  })
})

describe('reading words', () => {
  it('finds the kind of a step from its label', () => {
    expect(kindFromText('Searching the web')).toBe('search')
    expect(kindFromText('Opening bestbuy.com')).toBe('browse')
    expect(kindFromText('Clicking “Add to cart”')).toBe('click')
    expect(kindFromText('Typing your email')).toBe('type')
    expect(kindFromText('Reading notes.md')).toBe('file')
    expect(kindFromText('Running the script')).toBe('shell')
    expect(kindFromText('Remembering your size')).toBe('memory')
    expect(kindFromText('Scheduling a reminder')).toBe('schedule')
    expect(kindFromText('You took over')).toBe('user')
    expect(kindFromText('Thinking it over')).toBe('other')
  })

  it('finds the site a label names, and nothing else', () => {
    expect(hostFromText('Opened www.BestBuy.com')).toBe('bestbuy.com')
    expect(hostFromText('Read news.ycombinator.com')).toBe('news.ycombinator.com')
    expect(hostFromText('Typed alex@example.com')).toBeUndefined()
    expect(hostFromText('Searched for “example.com coupons”')).toBeUndefined()
    expect(hostFromText('Saved notes.md')).toBeUndefined()
  })

  it('takes the site out of a label under its name', () => {
    expect(stripHost('Compared prices on bestbuy.com', 'bestbuy.com')).toBe('Compared prices')
    expect(stripHost('Opened bestbuy.com', 'bestbuy.com')).toBe('Opened')
    expect(stripHost('Opened the listing', 'amazon.com')).toBe('Opened the listing')
  })

  it('lowers a reason’s first word only when it isn’t a name', () => {
    expect(reasonCase('Couldn’t sign in to united.com.')).toBe('couldn’t sign in to united.com')
    expect(reasonCase('United turned the password down')).toBe('United turned the password down')
  })

  it('uses the done label once a step is done, and the error when it failed', () => {
    const [ok] = toSteps(f.run14)
    expect(ok.label).toBe('Searched for “best noise cancelling headphones 2026”')
    const failed = toStep(f.run14[12])
    expect(failed.status).toBe('failed')
    expect(failed.label).toBe('Clicking “Check other stores”')
    expect(failed.error).toBe('A sign-up pop-up covered the button')
  })

  it('lists the sites in the order Alexa went to them', () => {
    expect(sitesOf(toSteps(f.failedSteps))).toEqual(['united.com'])
    expect(sitesOf(toSteps(f.chipSteps))).toEqual([])
  })
})
