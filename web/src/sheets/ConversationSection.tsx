import { useState } from 'react'
import type { SessionState } from '../api/types'
import { Button } from '../ui/Button'
import { ConfirmDialog } from '../ui/ConfirmDialog'
import { Group, Row, Section, useSettingId } from './parts'
import { notify } from './toasts'

export type ConversationProps = { session: SessionState; onCompact: () => Promise<void>; onReset: () => Promise<void> }

// ConversationSection is Settings' section about the one ongoing
// conversation: summarise it now, and, on its own at the bottom, clear it.
export function ConversationSection({ session, onCompact, onReset }: ConversationProps) {
  const ids = { compact: useSettingId('compact'), reset: useSettingId('reset') }
  const [busy, setBusy] = useState<'' | 'compact' | 'reset'>('')
  const [asking, setAsking] = useState(false)
  const idle = session.status !== 'working'
  const run = async (what: 'compact' | 'reset', fn: () => Promise<void>, done: string) => {
    setBusy(what)
    try {
      await fn()
      notify({ title: done, tone: 'ok' })
    } catch (x) {
      notify({ title: what === 'compact' ? 'Couldn’t summarise' : 'Couldn’t clear the conversation', body: (x as Error).message, tone: 'bad' })
    } finally {
      setBusy('')
    }
  }
  const times = session.compactions === 1 ? 'once' : `${session.compactions} times`
  return (
    <>
      <Section>
        <Group>
          <Row
            id={ids.compact}
            label="Summarise now"
            sub={
              <>
                When the conversation gets long, the older part is summarised so Alexa can carry on{session.compactions > 0 ? ` (${times} so far)` : ''}. Lasting facts go to Memory.
              </>
            }
          >
            <Button size="sm" loading={busy === 'compact'} disabled={!idle || !!busy} onClick={() => run('compact', onCompact, 'Summarised')}>
              Summarise
            </Button>
          </Row>
        </Group>
        {!idle && <p className="help after">Available when Alexa isn’t working.</p>}
      </Section>
      <Section title="Danger zone" className="danger-zone">
        <Group tone="danger">
          <Row id={ids.reset} label="Clear conversation" sub="Alexa forgets what you talked about. Memories, reports, files and schedules stay.">
            <Button size="sm" variant="danger" loading={busy === 'reset'} disabled={!idle || !!busy} onClick={() => setAsking(true)}>
              Clear
            </Button>
          </Row>
        </Group>
      </Section>
      <ConfirmDialog
        open={asking}
        title="Clear the conversation?"
        body="Alexa forgets what you talked about, and Reflect starts empty. Memories, reports, files and schedules stay. This can’t be undone."
        confirmLabel="Clear conversation"
        tone="danger"
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false)
          run('reset', onReset, 'Conversation cleared')
        }}
      />
    </>
  )
}
