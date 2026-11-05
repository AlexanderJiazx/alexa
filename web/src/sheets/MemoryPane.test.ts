import { describe, expect, it } from 'vitest'
import { memoryLine } from './MemoryPane'

describe('memoryLine', () => {
  const now = Date.now()

  it('says who put a memory there and when it last changed', () => {
    expect(memoryLine({ id: 1, content: 'x', source: 'user', createdAt: now - 5 * 3600e3 })).toBe('Added by you · 5h ago')
    expect(memoryLine({ id: 2, content: 'x', source: 'alexa', createdAt: now - 9 * 3600e3, updatedAt: now - 120e3 })).toBe('Learned by Alexa · 2m ago')
    expect(memoryLine({ id: 3, content: 'x', source: 'review', createdAt: now - 10e3 })).toBe('Learned by Alexa · just now')
  })

  it('reads one from a server without sources as learned', () => {
    expect(memoryLine({ id: 4, content: 'x', createdAt: now - 10e3 })).toBe('Learned by Alexa · just now')
  })
})
