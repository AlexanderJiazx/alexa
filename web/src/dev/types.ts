import type { ReactNode } from 'react'

// A gallery page: every file named gallery.tsx default-exports one. name is
// what ?gallery=<name> opens; render draws the page's fixtures.
export type GalleryPage = { name: string; title: string; render: () => ReactNode }

// sections are the usual shape of a page: a heading per fixture.
export type GallerySection = { title: string; node: ReactNode }
