import type { ReactNode } from 'react'

type Props = {
  title: string
  children: ReactNode
  className?: string
  // untitled leaves the heading off the screen (screen readers still get
  // it) when what's under it names itself, like the ask card's "Needs you".
  untitled?: boolean
}

// Section is one part of the home board: a list heading and what's under
// it. Each part is left out when it has nothing to show.
export function Section({ title, children, className, untitled }: Props) {
  return (
    <section className={className ? `home-sec ${className}` : 'home-sec'} aria-label={title}>
      {!untitled && <h2 className="list-head home-sec-head">{title}</h2>}
      {children}
    </section>
  )
}
