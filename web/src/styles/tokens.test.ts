import { describe, expect, it } from 'vitest'

// The stylesheets and components as written, so the rules in web/DESIGN.md
// are checked rather than trusted. Vitest empties stylesheets on import
// (even with ?raw), so they're read from disk; getBuiltinModule reaches
// Node's fs without bringing Node's types into the app.
type FS = { readFileSync(path: URL, encoding: 'utf8'): string }
const fs = (globalThis as unknown as { process: { getBuiltinModule(id: string): unknown } }).process.getBuiltinModule('node:fs') as FS
const read = (path: string) => fs.readFileSync(new URL(path, import.meta.url), 'utf8')
const readAll = (paths: string[]) => Object.fromEntries(paths.map((p) => [p, read(p)]))

const tokens = read('./tokens.css')
const css = readAll(Object.keys(import.meta.glob('../**/*.css')))
const code = readAll(Object.keys(import.meta.glob(['../**/*.tsx', '../**/*.ts', '!../**/*.test.*', '!../**/*.d.ts'])))

const stripComments = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '')
const norm = (v: string) => v.trim().toLowerCase().replace(/\s+/g, '')

// splitTop splits "a, b" at the comma outside any parentheses, so
// rgba(…, …) stays whole.
function splitTop(s: string): [string, string] {
  let depth = 0
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (c === '(') depth++
    else if (c === ')') depth--
    else if (c === ',' && depth === 0) return [s.slice(0, i), s.slice(i + 1)]
  }
  throw new Error(`no top-level comma in ${s}`)
}

// pairs is every light-dark() token: name → [light, dark].
function pairs(src: string) {
  const out = new Map<string, [string, string]>()
  for (const m of stripComments(src).matchAll(/(--[\w-]+):\s*light-dark\((.+)\);/g)) {
    const [a, b] = splitTop(m[2])
    out.set(m[1], [norm(a), norm(b)])
  }
  return out
}

// block is the declarations of the first rule after marker in the fallback.
function block(src: string, marker: string) {
  const plain = stripComments(src)
  const fallback = plain.slice(plain.indexOf('@supports not'))
  const start = fallback.indexOf(marker)
  if (start < 0) throw new Error(`no ${marker} in the fallback`)
  const body = fallback.slice(fallback.indexOf('{', start) + 1, fallback.indexOf('}', start))
  const out = new Map<string, string>()
  for (const m of body.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out.set(m[1], norm(m[2]))
  return out
}

const all = pairs(tokens)

describe('tokens.css', () => {
  it('has the colours as light-dark() pairs', () => {
    expect(all.size).toBeGreaterThanOrEqual(25)
    for (const name of ['--bg', '--surface', '--tile', '--line', '--line-strong', '--focus-line', '--ink', '--text-2', '--text-3', '--text-4']) expect(all.has(name), name).toBe(true)
  })

  // The language is monochrome: every colour is a grey (warm by a hair),
  // except the status colours, which only ever say a state.
  it('has no accent: only greys, besides status', () => {
    const status = new Set(['--ok', '--ok-bg', '--warn', '--warn-bg', '--bad', '--bad-bg'])
    const chroma = (v: string) => {
      const m = v.match(/^#([0-9a-f]{6})$/i) ?? v.match(/^rgba\((\d+),(\d+),(\d+),/)
      if (!m) throw new Error(`can't read ${v}`)
      const rgb = m.length === 2 ? [1, 3, 5].map((i) => parseInt(v.slice(i, i + 2), 16)) : [m[1], m[2], m[3]].map(Number)
      return Math.max(...rgb) - Math.min(...rgb)
    }
    for (const [name, pair] of all) {
      if (status.has(name)) continue
      for (const v of pair) expect(chroma(v), `${name} ${v}`).toBeLessThanOrEqual(8)
    }
  })

  // FE-16: without light-dark() the fallback must give the same colours, for
  // the system theme and for a theme pinned in Settings.
  it('restates every pair in the @supports fallback, light and dark', () => {
    const light = block(tokens, ':root {')
    const darkSystem = block(tokens, ':root:not([data-theme=light]) {')
    const darkPinned = block(tokens, ':root[data-theme=dark] {')
    expect([...light.keys()].sort()).toEqual([...all.keys()].sort())
    expect([...darkSystem.keys()].sort()).toEqual([...all.keys()].sort())
    expect([...darkPinned.keys()].sort()).toEqual([...all.keys()].sort())
    for (const [name, [l, d]] of all) {
      expect(light.get(name), `${name} light`).toBe(l)
      expect(darkSystem.get(name), `${name} dark`).toBe(d)
      expect(darkPinned.get(name), `${name} pinned dark`).toBe(d)
    }
  })
})

// WCAG 2 relative luminance and contrast ratio, for opaque hex colours.
function luminance(hex: string) {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
function contrast(a: string, b: string) {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p)
  return (x + 0.05) / (y + 0.05)
}

describe('contrast', () => {
  const surfaces = ['--bg', '--surface', '--tile']
  for (const [theme, i] of [['light', 0], ['dark', 1]] as const) {
    it(`keeps text-2 and text-3 at 4.5:1 or more on every surface (${theme})`, () => {
      for (const text of ['--ink', '--text-2', '--text-3'])
        for (const s of surfaces) {
          const ratio = contrast(all.get(text)![i], all.get(s)![i])
          expect(ratio, `${text} on ${s}: ${ratio.toFixed(2)}`).toBeGreaterThanOrEqual(4.5)
        }
    })

    // A control's edge (text-4) needs 3:1 against what it sits on.
    it(`keeps text-4 at 3:1 or more on the page and the surface (${theme})`, () => {
      for (const s of ['--bg', '--surface']) expect(contrast(all.get('--text-4')![i], all.get(s)![i]), s).toBeGreaterThanOrEqual(3)
    })
  }
})

// sRGB hex to and from Oklab, for what CSS mixes with color-mix(in oklab).
const toLinear = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
const toGamma = (c: number) => (c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055)
function oklab(hex: string) {
  const [r, g, b] = [1, 3, 5].map((i) => toLinear(parseInt(hex.slice(i, i + 2), 16) / 255))
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b)
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b)
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b)
  return [0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s, 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s, 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s]
}
function fromOklab([L, a, b]: number[]) {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3
  const rgb = [4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s, -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s, -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s]
  return '#' + rgb.map((c) => Math.round(Math.min(1, Math.max(0, toGamma(c))) * 255).toString(16).padStart(2, '0')).join('')
}

// declOf is a declaration of a rule in a stylesheet, as written.
function declOf(src: string, selector: string, prop: string) {
  const plain = stripComments(src)
  const at = plain.indexOf(`${selector} {`)
  if (at < 0) throw new Error(`no ${selector}`)
  const body = plain.slice(plain.indexOf('{', at) + 1, plain.indexOf('}', at))
  const m = body.match(new RegExp(`(?:^|;)\\s*${prop}:\\s*([^;]+)`))
  if (!m) throw new Error(`no ${prop} in ${selector}`)
  return m[1].trim()
}

// colourOf reads var(--x) or color-mix(in oklab, var(--a) p%, var(--b))
// in one theme.
function colourOf(value: string, i: 0 | 1): string {
  const mix = value.match(/^color-mix\(in oklab,\s*var\((--[\w-]+)\)\s+(\d+)%,\s*var\((--[\w-]+)\)\)$/)
  if (mix) {
    const [a, b] = [oklab(all.get(mix[1])![i]), oklab(all.get(mix[3])![i])]
    const p = Number(mix[2]) / 100
    return fromOklab(a.map((v, k) => v * p + b[k] * (1 - p)))
  }
  const v = value.match(/^var\((--[\w-]+)\)$/)
  if (!v) throw new Error(`can't read ${value}`)
  return all.get(v[1])![i]
}

describe('contrast of controls and highlights', () => {
  const compare = css['../features/blocks/compare/compare.css']
  const pickBest = '.b-compare td.best.is-pick .b-cell-v'
  for (const [theme, i] of [['light', 0], ['dark', 1]] as const) {
    // D7: an off switch is found by its frame, at 3:1, on the surfaces
    // switches sit on (a sheet, a panel, the page).
    it(`draws an off switch at 3:1 or more (${theme})`, () => {
      const ring = declOf(css['../ui/ui.css'], '.toggle', 'border').match(/var\((--[\w-]+)\)\s*$/)![1]
      for (const s of ['--bg', '--surface']) {
        const ratio = contrast(all.get(ring)![i], all.get(s)![i])
        expect(ratio, `${ring} on ${s}: ${ratio.toFixed(2)}`).toBeGreaterThanOrEqual(3)
      }
    })

    // D6: the best value in the pick's column is the cell the table draws
    // the eye to; its text stays at 4.5:1 on its wash.
    it(`keeps the pick's best value readable (${theme})`, () => {
      const fg = colourOf(declOf(compare, pickBest, 'color'), i)
      const bg = colourOf(declOf(compare, pickBest, 'background'), i)
      const ratio = contrast(fg, bg)
      expect(ratio, `${fg} on ${bg}: ${ratio.toFixed(2)}`).toBeGreaterThanOrEqual(4.5)
    })
  }
})

// The language is flat: things separate by a surface step and a 1px line,
// never by a shadow, a glow or a gradient that fakes depth.
describe('flat', () => {
  it('has no shadow in any stylesheet or component', () => {
    const shadow = /\b(box-shadow|text-shadow)\s*:|drop-shadow\(|\bboxShadow\b|\btextShadow\b/
    const offenders = [...Object.entries(css), ...Object.entries(code)]
      .flatMap(([path, src]) => stripComments(src).split('\n').map((line, n) => [path, n + 1, line] as const))
      .filter(([, , line]) => shadow.test(line))
      .map(([path, n, line]) => `${path}:${n} ${line.trim()}`)
    expect(offenders).toEqual([])
  })

  it('has no elevation, ring or accent tokens', () => {
    const gone = /--(e[0-3]|sh-[1-3]|hl|ring|ring-quiet|ring-ink|live|live-text|live-soft|info|info-bg)\b/
    const offenders = [...Object.entries(css), ...Object.entries(code)]
      .flatMap(([path, src]) => stripComments(src).split('\n').map((line, n) => [path, n + 1, line] as const))
      .filter(([, , line]) => gone.test(line))
      .map(([path, n, line]) => `${path}:${n} ${line.trim()}`)
    expect(offenders).toEqual([])
  })
})

describe('colour literals', () => {
  const literal = /#[0-9a-f]{3,8}\b|\brgba?\(|\bhsla?\(/i

  it('appear in no stylesheet but tokens.css', () => {
    expect(Object.keys(css).length).toBeGreaterThan(10)
    const offenders = Object.entries(css)
      .filter(([path]) => path !== './tokens.css')
      .flatMap(([path, src]) => stripComments(src).split('\n').map((line, n) => [path, n + 1, line] as const))
      .filter(([, , line]) => literal.test(line))
      .map(([path, n, line]) => `${path}:${n} ${line.trim()}`)
    expect(offenders).toEqual([])
  })

  // lib/theme.ts sets <meta name=theme-color>, which can't read a CSS
  // variable.
  it('appear in no component', () => {
    const offenders = Object.entries(code)
      .filter(([path]) => !path.endsWith('/lib/theme.ts'))
      .flatMap(([path, src]) => src.split('\n').map((line, n) => [path, n + 1, line] as const))
      .filter(([, , line]) => literal.test(line))
      .map(([path, n, line]) => `${path}:${n} ${line.trim()}`)
    expect(offenders).toEqual([])
  })
})

// A tap leaves :hover on until the next tap, so on a phone a hover wash
// would stick. Every :hover rule sits in @media (hover: hover).
describe('hover', () => {
  // ungated lists the selectors with :hover outside a hover media block.
  function ungated(src: string) {
    const out: string[] = []
    const open: string[] = []
    let head = ''
    for (const ch of stripComments(src)) {
      if (ch === '{') {
        const h = head.trim()
        if (h.includes(':hover') && !open.some((o) => /\(hover:\s*hover\)/.test(o))) out.push(h)
        open.push(h)
        head = ''
      } else if (ch === '}') {
        open.pop()
        head = ''
      } else if (ch === ';') head = ''
      else head += ch
    }
    return out
  }

  it('only applies where there is a pointer that hovers', () => {
    const offenders = Object.entries(css).flatMap(([path, src]) => ungated(src).map((s) => `${path} ${s}`))
    expect(offenders).toEqual([])
  })

  it('finds a hover rule outside the media block', () => {
    expect(ungated('.a:hover { color: red; } @media (hover: hover) { .b:hover { color: red; } }')).toEqual(['.a:hover'])
  })
})
