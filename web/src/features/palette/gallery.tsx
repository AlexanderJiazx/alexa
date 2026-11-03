import { api } from '../../api/client'
import type { Memory } from '../../api/types'
import { Header } from '../../app/Header'
import { OverlayHost } from '../../app/OverlayHost'
import { Stage } from '../../app/Stage'
import type { GalleryPage } from '../../dev/types'
import { fakeApi, feed, me, memories, notes } from '../../sheets/fixtures'
import { syncReflect } from '../../state/actions'
import { blankSession } from '../../state/reducer'
import { setApp, store } from '../../state/store'
import './gallery.css'
import { Palette } from './Palette'

// The chrome, live (development only): the real header, sheets, Reflect,
// palette and toasts over the real stage, answered from fixtures, so the
// keyboard, the focus and Undo can be tried with no backend. What the fake
// server was asked to do is kept on window.__calls.

type Calls = { kind: string; arg?: unknown }[]
const calls: Calls = []

let seeded = false
function seed() {
  if (seeded) return
  seeded = true
  fakeApi(false)
  // Memories the fake server really forgets, so Undo and the delayed
  // delete can be told apart.
  let kept: Memory[] = [...memories]
  Object.assign(api, {
    memories: async () => kept,
    deleteMemory: async (id: number) => {
      calls.push({ kind: 'deleteMemory', arg: id })
      kept = kept.filter((m) => m.id !== id)
      return {}
    },
    stop: async () => {
      calls.push({ kind: 'stop' })
      store.set((s) => ({ ...s, data: { ...s.data, session: { ...s.data.session, status: 'idle', activity: '' } } }))
      return {}
    },
  })
  ;(window as unknown as { __calls: Calls }).__calls = calls
  // As App does: Reflect follows the history entry, so Back closes it.
  window.addEventListener('popstate', syncReflect)
  setApp({ user: me, boot: 'ready', connection: 'live', config: { model: 'meta/llama-5-large', viewport: { width: 1280, height: 800 }, emailEnabled: false, shellEnabled: false, sandboxed: true, vapidPublicKey: '' } })
  store.set((s) => ({ ...s, data: { ...s.data, loaded: true, feed, notes, session: { ...blankSession, status: 'working', activity: 'Reading eurostar.com' } } }))
}

const page: GalleryPage = {
  name: 'chrome-live',
  title: 'Chrome, live',
  render() {
    seed()
    return (
      <div className="g-live app">
        <Header />
        <Stage />
        <OverlayHost />
        <Palette />
      </div>
    )
  },
}
export default page
