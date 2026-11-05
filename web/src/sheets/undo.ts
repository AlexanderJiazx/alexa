import { createStore, useSelector, type Store } from '../state/store'
import { keepFocusNear } from './focusNear'
import { toasts as appToasts, type Toasts } from './toasts'

// Undoable deletes (WP-12, FE-13, IX-F18). Forgetting a memory, deleting a
// schedule or a file hides it at once and shows "Undo" for a few seconds;
// only when that time is up is it deleted on the server. Undo just puts it
// back, so nothing has to be recreated and nothing changes its id. What
// can't be put back this way (a user, a login) asks first instead, with a
// ConfirmDialog.

// As long as a note with an action stays (toasts.ts actionMs).
export const undoMs = 10000

export type UndoInput = {
  // key names what's going, as kind:id ("memory:12"), so a list can hide it
  // while it waits.
  key: string
  title: string
  body?: string
  // commit does the delete for real.
  commit: () => Promise<unknown>
  // done runs after a commit that worked (reload the list); failed after
  // one that didn't (the item comes back).
  done?: () => void
  failed?: (e: Error) => void
}

export type Undo = {
  pending: Store<ReadonlySet<string>>
  remove: (u: UndoInput) => void
  // flush commits everything still waiting, at once (the page is going
  // away, or the user is signing out), and settles once every delete has.
  flush: () => Promise<void>
}

export function createUndo(toasts: Toasts, ms = undoMs): Undo {
  const pending = createStore<ReadonlySet<string>>(new Set())
  const waiting = new Map<string, { note: number; run: () => Promise<void> }>()
  const without = (key: string) =>
    pending.set((s) => {
      if (!s.has(key)) return s
      const n = new Set(s)
      n.delete(key)
      return n
    })

  function remove(u: UndoInput) {
    if (waiting.has(u.key)) return
    pending.set((s) => new Set(s).add(u.key))
    let settled = false
    const commit = (): Promise<void> => {
      if (settled) return Promise.resolve()
      settled = true
      waiting.delete(u.key)
      return u.commit().then(
        () => {
          u.done?.()
          without(u.key)
        },
        (e: Error) => {
          without(u.key)
          u.failed?.(e)
        },
      )
    }
    const note = toasts.notify({
      title: u.title,
      body: u.body,
      ms,
      action: {
        label: 'Undo',
        run: () => {
          settled = true
          waiting.delete(u.key)
          without(u.key)
        },
      },
      // Time up, dismissed or pushed out by a newer note: it goes for real.
      onEnd: (how) => {
        if (how !== 'action') commit()
      },
    })
    waiting.set(u.key, { note, run: commit })
  }

  return {
    pending,
    remove,
    flush() {
      const runs = [...waiting.values()].map((w) => {
        const p = w.run()
        toasts.dismiss(w.note, 'timeout')
        return p
      })
      return Promise.all(runs).then(() => {})
    },
  }
}

export const undo = createUndo(appToasts)

// removeLater is the app's undoable delete. The focus, on the delete
// button of a row that's about to go, moves to the row next to it.
export function removeLater(input: UndoInput) {
  keepFocusNear()
  undo.remove(input)
}

// keepingAlive runs fn with every fetch it starts marked keepalive, so a
// request sent as the page goes away outlives the page: a plain one can be
// cut off with it. The api's requests call fetch before their first await,
// so the deletes a flush starts all start inside fn.
export function keepingAlive<T>(fn: () => T): T {
  const plain = globalThis.fetch
  if (typeof plain !== 'function') return fn()
  globalThis.fetch = (input, init) => plain.call(globalThis, input, { ...init, keepalive: true })
  try {
    return fn()
  } finally {
    globalThis.fetch = plain
  }
}

// A page that's closing doesn't wait for its Undo: what was deleted goes
// now, in requests that outlive the page.
if (typeof window !== 'undefined') window.addEventListener('pagehide', () => void keepingAlive(() => undo.flush()))

// usePending returns the keys waiting to be deleted, for a list to hide.
export function usePending(): ReadonlySet<string> {
  return useSelector(undo.pending, (s) => s)
}
