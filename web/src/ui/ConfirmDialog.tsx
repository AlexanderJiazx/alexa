import type { ReactNode } from 'react'
import { Button } from './Button'
import { Dialog } from './Dialog'

type Props = {
  open: boolean
  title: string
  body?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  // danger is for what can't be undone: delete, clear, forget.
  tone?: 'primary' | 'danger'
  onConfirm: () => void
  onCancel: () => void
}

// ConfirmDialog asks before something consequential, in place of the
// browser's confirm(). Cancel takes the focus first, so Enter never
// destroys anything by accident.
export function ConfirmDialog({ open, title, body, confirmLabel = 'Confirm', cancelLabel = 'Cancel', tone = 'primary', onConfirm, onCancel }: Props) {
  return (
    <Dialog
      open={open}
      onClose={onCancel}
      title={title}
      actions={
        <>
          <Button autoFocus onClick={onCancel}>
            {cancelLabel}
          </Button>
          <Button variant="primary" className={tone === 'danger' ? 'danger' : undefined} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {body}
    </Dialog>
  )
}
