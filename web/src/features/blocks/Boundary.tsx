import { Component, type ReactNode } from 'react'
import type { Block } from '../../api/blocks'
import { Alert } from '../../ui/icons'
import { noun, summarize } from './summary'

// Boundary keeps one broken block from taking the message down with it:
// if a renderer throws, the block's text summary shows in its place (the
// RichBoundary pattern). Unknown types never reach it; they're skipped.
export class Boundary extends Component<{ block: Block; children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  componentDidCatch(err: unknown) {
    console.warn('A block failed to render', this.props.block?.type, err)
  }

  render() {
    return this.state.failed ? <Fallback block={this.props.block} /> : this.props.children
  }
}

// Fallback is what a block that can't be drawn shows: its summary, or a
// plain line saying so.
export function Fallback({ block }: { block: Block }) {
  const s = summarize(block)
  return (
    <p className="b-fallback">
      <Alert size={16} />
      <span>{s || `Couldn’t show this ${noun(block)}.`}</span>
    </p>
  )
}
