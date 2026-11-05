import { describe, expect, it } from 'vitest'

// WP-12's lint rules for the chrome: no inline style objects in the sheets
// and Reflect (styling lives in their stylesheets), and no browser
// confirm(), prompt() or alert() anywhere in the chrome (FE-13).
// Vitest empties CSS and raw imports, so the files are read from disk.

type FS = { readFileSync(path: URL, encoding: 'utf8'): string }
const fs = (globalThis as unknown as { process: { getBuiltinModule(id: string): unknown } }).process.getBuiltinModule('node:fs') as FS
const read = (path: string) => fs.readFileSync(new URL(path, import.meta.url), 'utf8')

const chrome = {
  ...import.meta.glob(['./**/*.tsx', '../reflect/**/*.tsx', '../auth/**/*.tsx', '../features/palette/**/*.tsx', '!./**/gallery.tsx', '!../**/gallery.tsx']),
}
const files = Object.keys(chrome).map((p) => ({ path: p, src: read(p) }))

describe('the chrome', () => {
  it('finds its files', () => {
    expect(files.length).toBeGreaterThan(10)
  })

  it('writes no inline style in sheets/ and reflect/', () => {
    const bad = files.filter((f) => /\bstyle=\{/.test(f.src)).map((f) => f.path)
    expect(bad).toEqual([])
  })

  it('never asks with the browser’s confirm, prompt or alert', () => {
    const bad = files.filter((f) => /(^|[^.\w])(confirm|prompt|alert)\(/m.test(f.src)).map((f) => f.path)
    expect(bad).toEqual([])
  })
})
