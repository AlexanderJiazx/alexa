import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { useExit } from '../lib/useExit'
import { Reflect } from '../reflect/Reflect'
import { AccountMenu } from '../sheets/AccountMenu'
import { overlayOf, sheets } from '../sheets/registry'
import { toasts as queue } from '../sheets/toasts'
import { closeMenu, closeSheet } from '../state/actions'
import { useUI, useUser } from '../state/selectors'
import { useSelector } from '../state/store'
import { Button } from '../ui/Button'
import { cx } from '../ui/cx'
import { Toast, ToastStack } from '../ui/Toast'

// OverlayHost shows what opens over the main screen: Reflect, the account
// menu, a sheet, and the toasts. Each plays its exit before it goes.
export function OverlayHost() {
  const { reflect, menu, sheet } = useUI((u) => ({ reflect: u.reflect, menu: u.menu, sheet: u.sheet }))
  const user = useUser()
  const m = useExit(menu && user ? true : null, 120)
  // The sheet or Settings stays mounted for its exit (it plays it while
  // open is false), then goes, so the next open starts fresh. Settings
  // moves between its sections itself (Users only for an admin).
  const s = useExit(overlayOf(sheet), 240)
  const Sheet = s.shown ? sheets[s.shown] : null
  return (
    <>
      <Reflect open={reflect} />
      {m.shown && user && <AccountMenu user={user} leaving={m.leaving} onClose={closeMenu} />}
      {Sheet && <Sheet key={s.shown} open={!s.leaving} onClose={closeSheet} />}
      <Toasts />
    </>
  )
}

// Toasts are the brief notes at the bottom: good news, bad news, and Undo.
// toast() in state/actions puts errors in the same queue, so every note
// looks and behaves the same.
export function Toasts() {
  const notes = useSelector(queue.store, (n) => n)
  const host = useTopModal()
  const stack = (
    <div className={cx('toast-host', host && 'in-modal')} onMouseEnter={queue.pause} onMouseLeave={queue.resume} onFocus={queue.pause} onBlur={queue.resume}>
      <ToastStack>
        {notes.map((n) => (
          <Toast
            key={n.id}
            title={n.title}
            body={n.body}
            tone={n.tone}
            leaving={n.leaving}
            onClose={() => queue.dismiss(n.id)}
            action={
              n.action && (
                <Button size="sm" variant="ghost" className="toast-act" onClick={() => queue.act(n.id)}>
                  {n.action.label}
                </Button>
              )
            }
          />
        ))}
      </ToastStack>
    </div>
  )
  // While a sheet, Settings or Reflect is open everything outside it is
  // inert, so the stack moves into it, where its Undo can still be pressed,
  // and sits at its foot.
  return host ? createPortal(stack, host) : stack
}

// useTopModal returns the topmost open sheet, Settings or Reflect, or null.
// A small dialog over it (a ConfirmDialog, the palette) is closed again
// before anything it does is reported, so a note never lands in one.
function useTopModal() {
  const [top, setTop] = useState<HTMLDialogElement | null>(null)
  useEffect(() => {
    const find = () => {
      const open = Array.from(document.querySelectorAll<HTMLDialogElement>('dialog.sheet[open], dialog.settings[open], dialog.reflect[open]')).filter((d) => d.matches(':modal'))
      setTop(open[open.length - 1] ?? null)
    }
    const mo = new MutationObserver(find)
    mo.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['open'] })
    find()
    return () => mo.disconnect()
  }, [])
  return top
}
