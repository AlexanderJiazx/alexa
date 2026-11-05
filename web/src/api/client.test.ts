import { afterEach, describe, expect, it, vi } from 'vitest'
import { submit } from '../composer/outbox'

// What the app sends for a late answer reaches the server with the entry
// of the question it answers (FE-1), through the outbox and the client.
describe('send', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('posts the skipped question a late answer answers', async () => {
    const bodies: Record<string, unknown>[] = []
    vi.stubGlobal('fetch', async (_: string, init: RequestInit) => {
      bodies.push(JSON.parse(String(init.body)))
      return new Response('{"ok":true}', { status: 200 })
    })
    const item = await submit({ text: 'About “When?”: Sat 14 Nov', via: 'form', values: { date: '2026-11-14' }, askEntry: 7 }, 'ask')
    expect(item.state).not.toBe('failed')
    expect(bodies).toHaveLength(1)
    expect(bodies[0]).toMatchObject({ text: 'About “When?”: Sat 14 Nov', via: 'form', values: { date: '2026-11-14' }, askEntry: 7 })
  })
})
