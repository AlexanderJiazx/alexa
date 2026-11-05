import { useId, useState } from 'react'
import { api } from '../api/client'
import type { Memory } from '../api/types'
import { firstLine } from '../features/palette/context'
import { ago } from '../lib/format'
import { useChanged } from '../state/selectors'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { Field, Input, TextArea } from '../ui/Field'
import { IconButton } from '../ui/IconButton'
import { Brain, Plus, Search, Trash } from '../ui/icons'
import { useIntent } from './intent'
import { failed, Group, LoadError, RowSkeletons, Section, useFirstFocus, useFocusBack, useList } from './parts'
import { removeLater, usePending } from './undo'

// Past this many memories, a search field helps find one.
const searchFrom = 10

// memoryLine says, under a memory, who put it there and when it last
// changed: the user, or Alexa as it learned.
export function memoryLine(m: Memory): string {
  const at = ago(Math.max(m.createdAt, m.updatedAt ?? 0))
  return m.source === 'user' ? `Added by you · ${at}` : `Learned by Alexa · ${at}`
}

// MemoryPane is Settings' Memory: what Alexa remembers about the user, for
// good. Add to it, change it in place, or forget something (with Undo).
export function MemoryPane() {
  const changed = useChanged('memory')
  const { list, error, reload } = useList(api.memories, [changed])
  const pending = usePending()
  const [text, setText] = useState('')
  const [adding, setAdding] = useState(false)
  const [query, setQuery] = useState('')
  const [editing, setEditing] = useState<{ id: number; content: string } | null>(null)
  const ids = useId()
  const back = useFocusBack(list)
  // Done changing one: the focus goes back to the memory.
  const stopEditing = (id: number) => {
    setEditing(null)
    back([`${ids}m${id}`])
  }
  const focusAdd = useIntent('memory') === 'new'
  const addId = useFirstFocus(focusAdd)

  const q = query.trim().toLowerCase()
  const shown = (list ?? []).filter((m) => !pending.has(`memory:${m.id}`) && (!q || m.content.toLowerCase().includes(q)))

  const add = async () => {
    const t = text.trim()
    if (!t || adding) return
    setAdding(true)
    try {
      await api.addMemory(t)
      setText('')
      reload()
    } catch (e) {
      failed('Couldn’t remember that')(e as Error)
    } finally {
      setAdding(false)
    }
  }

  const forget = (m: Memory) =>
    removeLater({
      key: `memory:${m.id}`,
      title: 'Forgotten',
      body: firstLine(m.content, 48),
      commit: () => api.deleteMemory(m.id),
      done: reload,
      failed: failed('Couldn’t forget that'),
    })

  return (
    <>
      <p className="intro">Things Alexa remembers about you, for good. It adds to this as it learns, and you can change or forget anything.</p>
      <form
        className="add-row"
        onSubmit={(e) => {
          e.preventDefault()
          add()
        }}
      >
        <Field label="Something to remember" id={addId} hideLabel flush>
          {(id) => <Input id={id} value={text} onChange={(e) => setText(e.target.value)} placeholder="Something to remember" />}
        </Field>
        <IconButton type="submit" label="Remember" disabled={!text.trim() || adding} className="add-btn">
          <Plus size={18} />
        </IconButton>
      </form>

      {list && list.length > searchFrom && (
        <div className="search-row">
          <Search size={16} />
          <Field label="Search memories" hideLabel flush>
            {(id) => <Input id={id} type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search memories" />}
          </Field>
        </div>
      )}

      <Section>
        {error && !list ? (
          <LoadError message={error} onRetry={reload} />
        ) : list === null ? (
          <RowSkeletons />
        ) : shown.length === 0 ? (
          q ? (
            <p className="empty-note">No memory mentions “{query.trim()}”.</p>
          ) : (
            <EmptyState icon={<Brain size={28} />} title="Nothing remembered yet" body="Tell Alexa something about you, like where you live or what you like, and it keeps it in mind from then on." />
          )
        ) : (
          <Group>
            <ul className="row-list">
              {shown.map((m) => (
                <li key={m.id} className="row memory">
                  {editing?.id === m.id ? (
                    <form
                      className="row-edit"
                      onSubmit={async (e) => {
                        e.preventDefault()
                        const content = editing.content.trim()
                        if (!content) return
                        try {
                          await api.updateMemory(m.id, content)
                          stopEditing(m.id)
                          reload()
                        } catch (x) {
                          failed('Couldn’t save that')(x as Error)
                        }
                      }}
                    >
                      <Field label="Edit memory" hideLabel flush>
                        {(id) => (
                          <TextArea
                            id={id}
                            autoFocus
                            rows={3}
                            value={editing.content}
                            onChange={(e) => setEditing({ id: m.id, content: e.target.value })}
                            onKeyDown={(e) => {
                              if (e.key === 'Escape') {
                                e.preventDefault()
                                e.stopPropagation()
                                stopEditing(m.id)
                              }
                            }}
                          />
                        )}
                      </Field>
                      <div className="row-btns">
                        <Button type="submit" size="sm" variant="primary">
                          Save
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => stopEditing(m.id)}>
                          Cancel
                        </Button>
                      </div>
                    </form>
                  ) : (
                    <>
                      <button type="button" id={`${ids}m${m.id}`} className="row-text rowlink" onClick={() => setEditing({ id: m.id, content: m.content })} aria-label={`Change: ${m.content}`}>
                        <span className="row-label quote">{m.content}</span>
                        <span className="row-sub num">{memoryLine(m)}</span>
                      </button>
                      <div className="row-end">
                        <IconButton size="sm" tone="muted" label="Forget" title="Forget" onClick={() => forget(m)}>
                          <Trash size={16} />
                        </IconButton>
                      </div>
                    </>
                  )}
                </li>
              ))}
            </ul>
          </Group>
        )}
      </Section>
    </>
  )
}
