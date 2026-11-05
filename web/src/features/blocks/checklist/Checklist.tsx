import type { BlockOf } from '../../../api/blocks'
import { Done, Failed, Skipped, Todo } from '../icons'
import { Head } from '../parts/Head'
import './checklist.css'

type Status = BlockOf<'checklist'>['items'][number]['status']

const marks = { done: Done, failed: Failed, todo: Todo, skipped: Skipped }
const words: Record<Status, string> = { done: 'done', failed: 'failed', todo: 'to do', skipped: 'skipped' }

// Checklist is what got done and what didn't: a tick, a cross, an empty
// ring for still to do and a dash for skipped, each with the word for
// screen readers, and a note under what needs explaining.
export function Checklist({ block: b }: { block: BlockOf<'checklist'> }) {
  const done = b.items.filter((i) => i.status === 'done').length
  return (
    <>
      <Head block={b} meta={`${done} of ${b.items.length} done`} />
      <ul className="b-surface b-check" role="list">
        {b.items.map((it, i) => {
          const M = marks[it.status] ?? Todo
          return (
            <li key={i} className={`s-${it.status}`}>
              <span className="b-check-mark">
                <M size={20} />
              </span>
              <span className="b-check-body">
                <span className="t">
                  {it.text}
                  <span className="sr">, {words[it.status] ?? it.status}</span>
                </span>
                {it.note && <span className="note">{it.note}</span>}
              </span>
            </li>
          )
        })}
      </ul>
    </>
  )
}
