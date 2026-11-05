import type { Money as M } from '../../../api/blocks'
import { moneyRuns } from '../../../lib/format'
import { cx } from '../../../ui/cx'

// Money is an amount in its currency, in the user's locale, with tabular
// figures and the symbol a touch smaller than the number.
export function Money({ amount, currency, cents, className }: M & { cents?: boolean; className?: string }) {
  return (
    <span className={cx('money', className)}>
      {moneyRuns(amount, currency, undefined, cents).map((p, i) =>
        p.cur ? (
          <span key={i} className="cur">
            {p.text}
          </span>
        ) : (
          p.text
        ),
      )}
    </span>
  )
}

const pers = { night: 'night', person: 'person', month: 'month', total: 'total' } as const

// Price is a card's price: the amount, what it's per, and what it was.
export function Price({ price }: { price: M & { was?: number; per?: keyof typeof pers } }) {
  const off = price.was && price.was > price.amount ? Math.round((1 - price.amount / price.was) * 100) : 0
  return (
    <span className="b-price">
      <Money amount={price.amount} currency={price.currency} className="now" />
      {price.per && <span className="per">{price.per === 'total' ? 'total' : `/ ${pers[price.per]}`}</span>}
      {off > 0 && (
        <>
          <s className="was">
            <span className="sr">was </span>
            <Money amount={price.was!} currency={price.currency} />
          </s>
          <span className="off">−{off}%</span>
        </>
      )}
    </span>
  )
}
