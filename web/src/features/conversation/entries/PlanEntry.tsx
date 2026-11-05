import { StepsBlock } from '../../progress/StepsBlock'
import type { EntryProps } from '../registry'

// PlanEntry is Alexa's plan for an exchange (entry.data.plan, Appendix
// B.1), updated in place as steps finish. The log and Reflect put it at the
// head of its run of steps, where it folds into the run's summary; on its
// own it's one quiet line ("Find headphones · 4 of 4") that opens to the
// checklist. While the run is on the main screen the RunCard shows it.
export function PlanEntry({ entry, mode }: EntryProps) {
  return <StepsBlock steps={[]} plan={entry} live={false} mode={mode} />
}
