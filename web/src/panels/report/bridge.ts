// The theme bridge for HTML reports (WP-09). A page Alexa writes runs in a
// sandboxed frame; the bridge is the little it shares with the app:
// - the design tokens, resolved to plain values, in <style id="alexa-theme">,
//   with alexa-base.css under them, so the page looks native in light and
//   dark without choosing a colour of its own;
// - a theme message ({type: 'alexa:theme', vars, scheme}) whenever the app's
//   theme changes, which the frame applies at once, without a reload, and
//   passes on to the page as an "alexa:theme" event so charts can redraw;
// - the page's height ({type: 'alexa:height', height}), so the panel scrolls
//   it like any other document;
// - alexa.send(text), which sends the user's choice as their own, visible
//   message: only on a tap or click, at most 300 characters and one every 2
//   seconds. The frame checks this for quick feedback and the app checks it
//   again, because any script in the page can post the message itself.

// The tokens a page can use. Colours are resolved through the cascade, so
// light-dark() and the theme Settings pins are already applied. Older token
// names are aliases in alexa-base.css, so older pages still look right.
export const colorTokens = [
  '--bg',
  '--surface',
  '--tile',
  '--line',
  '--line-strong',
  '--focus-line',
  '--ink',
  '--ink-2',
  '--text-2',
  '--text-3',
  '--text-4',
  '--on-ink',
  '--ok',
  '--ok-bg',
  '--warn',
  '--warn-bg',
  '--bad',
  '--bad-bg',
  '--series-1',
  '--series-2',
  '--series-3',
  '--hover',
  '--press',
  '--select',
]

export const plainTokens = [
  '--font',
  '--font-display',
  '--font-mono',
  '--t-micro',
  '--t-small',
  '--t-body',
  '--t-read',
  '--t-title',
  '--t-head',
  '--t-display',
  '--tr-micro',
  '--tr-body',
  '--tr-read',
  '--tr-title',
  '--tr-head',
  '--tr-display',
  '--tr-caps',
  '--s1',
  '--s2',
  '--s3',
  '--s4',
  '--s5',
  '--s6',
  '--s8',
  '--s10',
  '--s14',
  '--r-xs',
  '--r-sm',
  '--r-md',
  '--r-lg',
  '--r-xl',
  '--r-full',
  '--d-1',
  '--d-2',
  '--d-3',
  '--ease-out',
  '--measure',
]

export type Scheme = 'light' | 'dark'
export type Theme = { vars: Record<string, string>; scheme: Scheme }

// currentScheme is the theme the app shows: the one Settings pinned, or
// the system's.
export function currentScheme(doc: Document = document): Scheme {
  const pinned = doc.documentElement.dataset.theme
  if (pinned === 'light' || pinned === 'dark') return pinned
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

// readTheme resolves the tokens as the app shows them right now. A probe
// element turns each colour (a light-dark() pair, or a var() of one) into
// the colour on screen.
export function readTheme(doc: Document = document): Theme {
  const root = getComputedStyle(doc.documentElement)
  const probe = doc.createElement('span')
  probe.hidden = true
  doc.body.appendChild(probe)
  const vars: Record<string, string> = {}
  try {
    const cs = getComputedStyle(probe)
    for (const name of colorTokens) {
      probe.style.color = `var(${name})`
      vars[name] = cs.color
    }
  } finally {
    probe.remove()
  }
  for (const name of plainTokens) {
    const v = root.getPropertyValue(name).trim()
    if (v) vars[name] = v
  }
  return { vars, scheme: currentScheme(doc) }
}

// A token's value may not end the rule or open markup.
const safeName = /^--[a-z0-9-]+$/
const safeValue = (v: string) => !/[{}<>;]/.test(v)

// themeCSS is the theme as the rule in <style id="alexa-theme">.
export function themeCSS({ vars, scheme }: Theme) {
  const decls = Object.entries(vars)
    .filter(([k, v]) => safeName.test(k) && safeValue(v))
    .map(([k, v]) => `${k}:${v}`)
  return `:root{color-scheme:${scheme === 'dark' ? 'dark' : 'light'};${decls.join(';')}}`
}

// watchTheme calls fn whenever the app's theme changes: Settings pins one
// (data-theme on <html>) or the system switches.
export function watchTheme(fn: () => void) {
  const mo = new MutationObserver(fn)
  mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
  const mq = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null
  mq?.addEventListener('change', fn)
  return () => {
    mo.disconnect()
    mq?.removeEventListener('change', fn)
  }
}

// ---------- the frame ----------

// The page's height is clamped to this range in the panel; a taller page
// scrolls inside its frame.
export const minHeight = 160
export const maxHeight = 8000
export const clampHeight = (h: number) => Math.min(maxHeight, Math.max(minHeight, Math.ceil(h)))

export const maxSend = 300
export const sendGap = 2000

// frameScript runs in the frame before the page's own scripts. It's kept
// to old, plain JavaScript, as it runs in whatever the page brings.
export const frameScript = `(function () {
  var style = document.getElementById('alexa-theme');
  var name = /^--[a-z0-9-]+$/;
  var bad = /[{}<>;]/;
  function css(vars, scheme) {
    var out = [];
    for (var k in vars) if (name.test(k) && typeof vars[k] === 'string' && !bad.test(vars[k])) out.push(k + ':' + vars[k]);
    return ':root{color-scheme:' + (scheme === 'dark' ? 'dark' : 'light') + ';' + out.join(';') + '}';
  }
  var waiting = {};
  var seq = 0;
  var last = 0;
  addEventListener('message', function (e) {
    if (e.source !== parent) return;
    var d = e.data || {};
    if (d.type === 'alexa:theme' && d.vars) {
      if (style) style.textContent = css(d.vars, d.scheme);
      var ev;
      try { ev = new CustomEvent('alexa:theme', { detail: { vars: d.vars, scheme: d.scheme } }); } catch (x) { return; }
      dispatchEvent(ev);
    } else if (d.type === 'alexa:sent' && waiting[d.id]) {
      waiting[d.id]({ ok: !!d.ok, error: d.error });
      delete waiting[d.id];
    }
  });
  function send(text) {
    return new Promise(function (done) {
      var ua = navigator.userActivation;
      text = String(text == null ? '' : text).replace(/\\s+/g, ' ').trim();
      if (!ua || !ua.isActive) return done({ ok: false, error: 'alexa.send needs a tap or click' });
      if (!text) return done({ ok: false, error: 'Nothing to send' });
      if (text.length > ${maxSend}) return done({ ok: false, error: 'Keep it under ${maxSend} characters' });
      var now = Date.now();
      if (now - last < ${sendGap}) return done({ ok: false, error: 'One message every ${sendGap / 1000} seconds' });
      last = now;
      var id = ++seq;
      waiting[id] = done;
      parent.postMessage({ type: 'alexa:send', id: id, text: text }, '*');
    });
  }
  try { Object.defineProperty(window, 'alexa', { value: Object.freeze({ send: send }) }); } catch (x) {}
  var height = 0;
  function size() {
    var h = Math.ceil(document.documentElement.getBoundingClientRect().height);
    if (h && h !== height) {
      height = h;
      parent.postMessage({ type: 'alexa:height', height: h }, '*');
    }
  }
  if (window.ResizeObserver) new ResizeObserver(size).observe(document.documentElement);
  addEventListener('load', size);
})();`

// prelude is what goes in the frame's head after the policy: Alexa's
// fonts, the base styles, the theme and the bridge.
export function prelude(theme: Theme, base: string) {
  const fonts = 'https://fonts.googleapis.com/css2?family=Instrument+Sans:wght@400;500;600&family=Instrument+Serif:ital@0;1&display=swap'
  return (
    `<link rel="stylesheet" href="${fonts}">` +
    `<style id="alexa-base">${base.replace(/<\/style/gi, '<\\/style')}</style>` +
    `<style id="alexa-theme">${themeCSS(theme)}</style>` +
    `<script>${frameScript}</script>`
  )
}

// ---------- alexa.send, on the app's side ----------

export type SendCheck = { ok: true; text: string } | { ok: false; error: string }

// checkSend decides whether a message from a report goes out: only while
// the user is interacting (active), only text of 1 to 300 characters, and
// only 2 seconds after the last one that went (last).
export function checkSend(raw: unknown, active: boolean, now: number, last: number): SendCheck {
  if (!active) return { ok: false, error: 'alexa.send needs a tap or click' }
  const text = typeof raw === 'string' ? raw.replace(/\s+/g, ' ').trim() : ''
  if (!text) return { ok: false, error: 'Nothing to send' }
  if (text.length > maxSend) return { ok: false, error: `Keep it under ${maxSend} characters` }
  if (now - last < sendGap) return { ok: false, error: `One message every ${sendGap / 1000} seconds` }
  return { ok: true, text }
}

// The app's own clock for alexa.send, shared by every report frame, so
// reopening a report doesn't reset it.
let lastSent = -Infinity

// gateSend checks a message from a report against the user's activation
// (it reaches the app from a tap in the frame) and the shared clock, and
// starts the clock when it lets one through.
export function gateSend(raw: unknown, now = Date.now()): SendCheck {
  const active = typeof navigator !== 'undefined' && !!navigator.userActivation?.isActive
  const res = checkSend(raw, active, now, lastSent)
  if (res.ok) lastSent = now
  return res
}

export type FrameMessage = { type: 'alexa:height'; height: number } | { type: 'alexa:send'; id: number; text: string }

// frameMessage reads what a report frame posted, or null for anything else.
export function frameMessage(d: unknown): FrameMessage | null {
  if (!d || typeof d !== 'object') return null
  const m = d as Record<string, unknown>
  if (m.type === 'alexa:height' && typeof m.height === 'number' && Number.isFinite(m.height)) return { type: 'alexa:height', height: m.height }
  if (m.type === 'alexa:send' && typeof m.id === 'number') return { type: 'alexa:send', id: m.id, text: typeof m.text === 'string' ? m.text : '' }
  return null
}
