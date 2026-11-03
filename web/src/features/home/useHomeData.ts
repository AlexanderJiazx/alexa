import { useEffect, useMemo, useState } from 'react'
import { api } from '../../api/client'
import type { Report, Schedule } from '../../api/types'
import { useChanged } from '../../state/selectors'
import { useApp } from '../../state/store'

// What the home board shows besides the session: the schedules (for what
// runs next) and the reports, from the endpoints the sheets and Reflect
// already use.
export type HomeData = { schedules: Schedule[] | null; reports: Report[] | null }

// The last answers are kept for the session, so coming back home draws the
// board at once from them while it refreshes.
const cache: HomeData = { schedules: null, reports: null }

// What the reports lists said, in the server's time only (a client's clock
// can be off): the newest report in the last one, and the reports an
// earlier list had that a later one didn't (deleted since).
let newest = 0
const gone = new Set<string>()

function listed(list: Report[]) {
  const ids = new Set(list.map((r) => r.id))
  for (const r of cache.reports ?? []) if (!ids.has(r.id)) gone.add(r.id)
  newest = list.reduce((m, r) => Math.max(m, r.updatedAt), 0)
}

export function useHomeData(): HomeData {
  const [d, setD] = useState<HomeData>(cache)
  const sch = useChanged('schedules')
  const rep = useChanged('reports') + useChanged('reportDeleted')
  // Reports that arrived over the socket since the list was fetched.
  const live = useApp((s) => s.data.reports)

  useEffect(() => load('schedules', api.schedules, setD), [sch])
  useEffect(() => load('reports', api.reports, setD), [rep])

  const reports = useMemo(() => {
    if (!d.reports) return null
    const byId = new Map(d.reports.map((r) => [r.id, r]))
    for (const r of Object.values(live)) {
      const had = byId.get(r.id)
      // One the list doesn't have is new only if it's newer than anything
      // in the list and no list has dropped it; otherwise it was deleted.
      if (had ? had.updatedAt <= r.updatedAt : !gone.has(r.id) && r.updatedAt > newest) byId.set(r.id, r)
    }
    return [...byId.values()]
  }, [d.reports, live])

  return { ...d, reports }
}

function load<K extends keyof HomeData>(key: K, get: () => Promise<NonNullable<HomeData[K]>>, set: (d: HomeData) => void) {
  let on = true
  get()
    .then((v) => {
      if (key === 'reports') listed(v as Report[])
      cache[key] = v
      if (on) set({ ...cache })
    })
    .catch(() => {})
  return () => {
    on = false
  }
}
