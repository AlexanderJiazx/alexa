// How the head shows where the page is: the host in ink, the rest of the
// address quieter, and whether the connection is private.

export type Address = { host: string; rest: string; secure: boolean; blank: boolean }

export function addressOf(url: string): Address {
  if (!url || url === 'about:blank') return { host: '', rest: '', secure: true, blank: true }
  try {
    const u = new URL(url)
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return { host: url, rest: '', secure: true, blank: false }
    const path = u.pathname === '/' ? '' : u.pathname
    return { host: u.host.replace(/^www\./, ''), rest: path + u.search, secure: u.protocol === 'https:', blank: false }
  } catch {
    return { host: url, rest: '', secure: true, blank: false }
  }
}

// Second-level labels people register under in a country's domain
// (bbc.co.uk, amazon.com.au), and hosting suffixes whose next label is the
// site itself (alex.github.io): the common ones, not the whole public
// suffix list. For one missing here only its last two labels are sure to
// stay whole, as for any other host.
const secondLevel = new Set(['co', 'com', 'net', 'org', 'gov', 'edu', 'ac', 'or', 'ne', 'go', 'gob', 'gv', 'ltd', 'plc', 'sch', 'nhs', 'mil', 'nom'])
const hosting = new Set(['github.io', 'gitlab.io', 'herokuapp.com', 'vercel.app', 'netlify.app', 'pages.dev', 'workers.dev', 'web.app', 'firebaseapp.com', 'appspot.com', 'blogspot.com', 'azurewebsites.net', 'cloudfront.net', 'fly.dev', 'onrender.com', 'glitch.me', 'repl.co', 'ngrok.io', 'ngrok-free.app'])

export type HostParts = { sub: string; site: string }

// hostParts splits a host into its site, the name someone registered
// (ycombinator.com, bbc.co.uk), and what comes before it ("news."). Short
// of room the head cuts the part before first and the site last: the site
// is what tells bestbuy.com from evilbuy.com, and
// accounts.example.com.evil.io from example.com.
export function hostParts(host: string): HostParts {
  const [, name, port = ''] = host.match(/^(.*?)(:\d+)?$/) ?? [host, host]
  // An address, a bracketed IPv6 one or a single name is the site whole.
  if (!name.includes('.') || name.startsWith('[') || /^[\d.]+$/.test(name)) return { sub: '', site: host }
  const labels = name.split('.')
  const n = labels.length
  let keep = 2
  if (hosting.has(labels.slice(-2).join('.'))) keep = 3
  else if (n >= 3 && /^[a-z]{2}$/i.test(labels[n - 1]) && secondLevel.has(labels[n - 2].toLowerCase())) keep = 3
  if (n <= keep) return { sub: '', site: host }
  return { sub: labels.slice(0, n - keep).join('.') + '.', site: labels.slice(n - keep).join('.') + port }
}

// clickCount is which click of a double or triple click a press is: the
// same button pressed again soon, close to the last press. Pointer events
// don't count clicks (their detail is 0), so the view does, and a double
// click in take-over still selects a word.
export type Press = { at: number; x: number; y: number; button: number; count: number }

export function clickCount(prev: Press | null, at: number, x: number, y: number, button: number): number {
  if (!prev || prev.button !== button || at - prev.at > 500 || Math.hypot(x - prev.x, y - prev.y) > 6) return 1
  return prev.count >= 3 ? 1 : prev.count + 1
}
