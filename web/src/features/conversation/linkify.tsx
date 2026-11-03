import type { ReactNode } from 'react'
import { LinkChip } from '../blocks/LinkChip'

// linkify turns bare URLs in Alexa's messages into site chips: the site's
// icon, its host and a short path, instead of a raw address.
export function linkify(text: string): ReactNode[] {
  const parts = text.split(/(https?:\/\/[^\s)]+[^\s).,;:!?'"])/g)
  return parts.map((part, i) => (i % 2 ? <LinkChip key={i} url={part} /> : part))
}
