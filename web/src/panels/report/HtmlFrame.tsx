import { useEffect, useMemo, useRef, useState } from 'react'
import base from '../../../public/alexa-base.css?raw'
import type { Report } from '../../api/types'
import { clampHeight, frameMessage, gateSend, prelude, readTheme, watchTheme } from './bridge'
import { withHead } from './html'

type Props = {
  report: Report
  // onSend sends what the page passed to alexa.send, once it's allowed, as
  // the user's visible message.
  onSend: (text: string) => void | Promise<boolean>
}

// HtmlFrame shows an HTML report in its sandbox, with the theme bridge: the
// page gets the app's tokens and keeps them in step with the theme, tells
// the panel how tall it is, and can send the user's choice back.
export function HtmlFrame({ report, onSend }: Props) {
  const ref = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState<number | null>(null)
  const send = useRef(onSend)
  send.current = onSend
  // The page is built once per revision, with the theme of that moment; a
  // later theme change is a message, not a reload.
  const doc = useMemo(() => withHead(report.content || '', prelude(readTheme(), base)), [report.content])

  const postTheme = () => ref.current?.contentWindow?.postMessage({ type: 'alexa:theme', ...readTheme() }, '*')
  useEffect(() => watchTheme(() => ref.current?.contentWindow?.postMessage({ type: 'alexa:theme', ...readTheme() }, '*')), [])

  useEffect(() => {
    const on = (e: MessageEvent) => {
      const win = ref.current?.contentWindow
      if (!win || e.source !== win) return
      const m = frameMessage(e.data)
      if (!m) return
      if (m.type === 'alexa:height') {
        setHeight(clampHeight(m.height))
        return
      }
      const res = gateSend(m.text)
      win.postMessage({ type: 'alexa:sent', id: m.id, ok: res.ok, error: res.ok ? undefined : res.error }, '*')
      if (res.ok) send.current(res.text)
    }
    addEventListener('message', on)
    return () => removeEventListener('message', on)
  }, [])

  return (
    <iframe
      ref={ref}
      className="rep-iframe"
      title={report.title}
      sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox"
      referrerPolicy="no-referrer"
      srcDoc={doc}
      onLoad={postTheme}
      // The page's own height, measured inside the frame.
      style={height ? { height } : undefined}
    />
  )
}
