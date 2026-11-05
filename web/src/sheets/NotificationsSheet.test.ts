import { describe, expect, it } from 'vitest'
import type { AppNotification } from '../api/types'
import { keepNew } from './NotificationsSheet'

// FH-10: opening the inbox marks everything read on the server, and the
// list comes back read a moment later. What was new keeps its dot until
// the inbox closes.

const note = (id: number, read: boolean): AppNotification => ({ id, title: `Note ${id}`, body: '', read, createdAt: id })

describe('keepNew', () => {
  it('keeps what was new when the inbox opened once the server says it’s read', () => {
    const opened = keepNew(new Set(), [note(3, false), note(2, false), note(1, true)])
    expect([...opened]).toEqual([3, 2])
    const after = keepNew(opened, [note(3, true), note(2, true), note(1, true)])
    expect(after).toBe(opened)
  })

  it('adds one that arrives while it’s open', () => {
    const opened = keepNew(new Set(), [note(1, false)])
    expect([...keepNew(opened, [note(2, false), note(1, true)])]).toEqual([1, 2])
  })

  it('is empty when nothing was new', () => {
    expect(keepNew(new Set(), [note(1, true)]).size).toBe(0)
  })
})
