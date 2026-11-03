import type { ReactNode } from 'react'

// Section is one labelled fixture on a gallery page.
export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="g-section">
      <h2>{title}</h2>
      <div className="g-body">{children}</div>
    </section>
  )
}
