// Ranking for the command palette. Pure, so it's tested without a DOM.

export type Searchable = { title: string; keywords?: string }

// fold lowers case and drops accents, so "resume" finds "Résumé".
export const fold = (s: string) => s.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()

// isSubsequence says whether every character of q appears in s, in order:
// "nsch" finds "New schedule".
function isSubsequence(q: string, s: string) {
  let i = 0
  for (const c of s) if (c === q[i] && ++i === q.length) return true
  return q.length === 0
}

// score is how well a command matches what was typed: 0 is no match. A
// title that starts with the query beats one with a word that does, which
// beats a match anywhere, then the keywords, then the letters in order.
export function score(item: Searchable, query: string): number {
  const q = fold(query.trim())
  if (!q) return 1
  const title = fold(item.title)
  const words = title.split(/[^\p{L}\p{N}]+/u).filter(Boolean)
  const keys = fold(item.keywords ?? '')
  if (title === q) return 100
  if (title.startsWith(q)) return 80
  if (words.some((w) => w.startsWith(q))) return 60
  if (title.includes(q)) return 40
  if (keys.split(/\s+/).some((k) => k && k.startsWith(q))) return 30
  if (keys.includes(q)) return 20
  // Several words match when each starts a word of the title or keywords:
  // "dark mode" finds "Switch to dark".
  const parts = q.split(/\s+/)
  if (parts.length > 1) {
    const all = [...words, ...keys.split(/\s+/)]
    return parts.every((p) => all.some((w) => w.startsWith(p))) ? 15 : 0
  }
  if (isSubsequence(q, title)) return 10
  return 0
}

// filterCommands keeps the commands that match, best first; equal matches
// keep their order, so the list doesn't jump about as you type.
export function filterCommands<T extends Searchable>(items: T[], query: string): T[] {
  return items
    .map((item, i) => ({ item, i, s: score(item, query) }))
    .filter((x) => x.s > 0)
    .sort((a, b) => b.s - a.s || a.i - b.i)
    .map((x) => x.item)
}
