import type { HTMLAttributes } from 'react'
import { cx } from './cx'

type Props = HTMLAttributes<HTMLDivElement> & {
  // interactive cards lift on hover and press in; put them on a <button> or
  // <a> with cardClass instead when the whole card is the action.
  interactive?: boolean
  pad?: 'none' | 'sm' | 'md' | 'lg'
  // flat drops the shadow, for cards inside cards.
  flat?: boolean
}

export function cardClass(interactive = false, pad: Props['pad'] = 'md', flat = false) {
  return cx('card', interactive && 'interactive', pad !== 'md' && `pad-${pad}`, flat && 'flat')
}

// Card is a surface that sits on the page: surface-1, a hairline and e1.
export function Card({ interactive, pad = 'md', flat, className, ...rest }: Props) {
  return <div className={cx(cardClass(interactive, pad, flat), className)} {...rest} />
}
