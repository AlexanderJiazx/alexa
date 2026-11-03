import type { ReactNode } from 'react'

// Kbd is a key or shortcut hint: <Kbd>⌘K</Kbd>. Desktop only; hide it on
// touch screens with .kbd-hint on its container.
export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="kbd">{children}</kbd>
}
