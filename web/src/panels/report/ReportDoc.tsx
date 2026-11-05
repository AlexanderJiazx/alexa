import { useMemo, type RefObject } from 'react'
import type { Report } from '../../api/types'
import { BlockList } from '../../features/blocks/BlockList'
import { useNow } from '../../ui/Timer'
import { Events, Files, Items, Sources } from './Extras'
import { withoutTitle } from './html'
import { Prose } from './Prose'
import { parts, prose, readMinutes, setTitle, splitLede, wordCount, written } from './text'

type Props = {
  report: Report
  // The title, watched by the panel so the head can take it over once it
  // scrolls away.
  titleRef: RefObject<HTMLHeadingElement | null>
  // onSend sends a reply card's text as a message from the report.
  onSend?: (text: string) => void | Promise<boolean>
}

function Eyebrow({ report }: { report: Report }) {
  const now = useNow()
  const min = readMinutes(wordCount(prose(report)))
  return (
    <p className="rep-eyebrow">
      {written(report.updatedAt, now)}
      {min > 0 && <span> · {min} min read</span>}
    </p>
  )
}

// ReportDoc is a report set as an article: the date and reading time, the
// serif title, a lede, the key rows, the content and its blocks, then the
// dates, files and sources (review-visual 5.7).
export function ReportDoc({ report, titleRef, onSend }: Props) {
  const d = report.data ?? {}
  // The body is laid out once per revision of the report, not on every
  // render (FE-06).
  const { lede, body } = useMemo(() => {
    const all = parts(report.content ? withoutTitle(report.content, report.title) : '', d.blocks)
    const first = all[0]
    if (first?.kind !== 'md') return { lede: '', body: all }
    const s = splitLede(first.text)
    if (!s.lede) return { lede: '', body: all }
    return { lede: s.lede, body: s.rest.trim() ? [{ kind: 'md' as const, text: s.rest }, ...all.slice(1)] : all.slice(1) }
  }, [report.content, report.title, d.blocks])
  // Without a lede of its own, the takeaway Alexa wrote for its card opens
  // the report.
  const takeaway = !lede && d.takeaway ? d.takeaway : ''
  return (
    <article className="report" aria-labelledby={`rep-${report.id}`}>
      <header className="rep-top">
        <Eyebrow report={report} />
        <h1 id={`rep-${report.id}`} ref={titleRef}>
          {setTitle(report.title)}
        </h1>
        {lede && <Prose text={lede} lede />}
        {takeaway && (
          <div className="prose lede">
            <p>{takeaway}</p>
          </div>
        )}
      </header>
      <Items items={d.items ?? []} />
      {body.map((p, i) =>
        p.kind === 'md' ? (
          <Prose key={i} text={p.text} />
        ) : (
          <div key={i} className="rep-blocks">
            {/* A reply card in a report goes as a message sent from the
                report, so it carries on the exchange the report belongs to. */}
            <BlockList blocks={p.blocks} placement="report" onReply={onSend} />
          </div>
        ),
      )}
      <Events events={d.events ?? []} />
      <Files files={d.files ?? []} />
      <Sources sources={d.sources ?? []} />
    </article>
  )
}
