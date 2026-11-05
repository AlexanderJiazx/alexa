// withoutTitle drops a leading heading that just repeats the report's
// title, which is already shown above the content.
export function withoutTitle(md: string, title: string) {
  const m = md.match(/^\s*#{1,2}\s+(.+)\n+/)
  if (!m) return md
  const norm = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '')
  const a = norm(m[1])
  const b = norm(title)
  const words = (s: string) => new Set(s.toLowerCase().split(/[^a-z0-9]+/).filter((w) => w.length > 2))
  const wa = words(m[1])
  const wb = words(title)
  const shared = [...wa].filter((w) => wb.has(w)).length
  return a === b || shared >= Math.min(wa.size, wb.size) * 0.6 ? md.slice(m[0].length) : md
}

export const isImage = (f: string) => /\.(png|jpe?g|webp|gif|svg|avif)$/i.test(f)

// HTML reports run in a frame sandboxed without allow-same-origin: an
// opaque origin with no access to Alexa, the user's session or the app's
// storage. The policy below also stops them fetching anything (no
// connect-src), and limits scripts to inline code and three well-known
// CDNs. It doesn't seal the page off: images and fonts load from any https
// address, links open in new tabs and the page can navigate itself, so a
// page can still reach the web through them. What it may send back to
// Alexa goes through the bridge (bridge.ts) as a visible message.
export const csp =
  "default-src 'none'; img-src data: blob: https:; media-src data: blob: https:; style-src 'unsafe-inline' https://fonts.googleapis.com; font-src data: https://fonts.gstatic.com; " +
  "script-src 'unsafe-inline' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com https://unpkg.com"

// withHead puts a prelude (the policy, then the theme and the bridge) at
// the very start of an HTML report, in a head of its own, rather than
// searching the page for its <head> (FE-05: a search for "<head" also finds
// <header>, a comment or a string in a script). The HTML parser does the
// rest:
// - our doctype comes first, so the page renders in standards mode;
// - the page's own doctype and <head> tag come too late and are ignored;
// - its <html> attributes (lang, dir) are kept on the root;
// - its <meta>, <title>, <link>, <style> and <script> before the body move
//   into our head, after the policy.
// Nothing in the page can come before the policy.
export function withHead(html: string, prelude: string) {
  return `<!doctype html><head><meta http-equiv="Content-Security-Policy" content="${csp}">${prelude}</head>${html}`
}

// withCSP is an HTML report with just the policy, as the panel showed it
// before the theme bridge.
export const withCSP = (html: string) => withHead(html, '')
