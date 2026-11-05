// Painting the live view's frames. Frames go straight from the socket onto
// a canvas, decoded off the main thread by createImageBitmap, so a frame
// never touches React (B09, FE-29): the view re-renders when its state
// changes, not 10 times a second.

type Decoded = { img: CanvasImageSource; w: number; h: number; close(): void }

// decode turns a JPEG blob into something drawImage takes. createImageBitmap
// decodes off the main thread; browsers without it get an <img>.
async function decode(blob: Blob): Promise<Decoded> {
  if (typeof createImageBitmap === 'function') {
    const b = await createImageBitmap(blob)
    return { img: b, w: b.width, h: b.height, close: () => b.close() }
  }
  const url = URL.createObjectURL(blob)
  const img = new Image()
  img.src = url
  try {
    await img.decode()
  } catch (e) {
    URL.revokeObjectURL(url)
    throw e
  }
  return { img, w: img.naturalWidth, h: img.naturalHeight, close: () => URL.revokeObjectURL(url) }
}

export type Painter = {
  push(frame: Blob): void
  // redraw paints the last frame again, onto a canvas that was just
  // attached.
  redraw(): void
  dispose(): void
}

// createPainter keeps a one-frame mailbox: while a frame decodes, only the
// newest to arrive meanwhile waits, so a burst never queues up and the last
// frame (the settled page) is never the one dropped (B08). While the tab is
// hidden nothing is decoded; the newest frame is painted when it's back.
export function createPainter(target: () => HTMLCanvasElement | null, onPaint: () => void): Painter {
  let next: Blob | null = null
  let last: Blob | null = null
  let busy = false
  let gone = false

  const draw = (d: Decoded) => {
    const c = target()
    if (!c) return
    if (c.width !== d.w || c.height !== d.h) {
      c.width = d.w
      c.height = d.h
    }
    c.getContext('2d', { alpha: false })?.drawImage(d.img, 0, 0)
  }

  const pump = async () => {
    if (busy) return
    busy = true
    while (next && !gone && !document.hidden) {
      const blob = next
      next = null
      try {
        const d = await decode(blob)
        if (!gone) {
          draw(d)
          last = blob
          onPaint()
        }
        d.close()
      } catch {
        // A frame that won't decode is skipped; the next one replaces it.
      }
    }
    busy = false
  }

  const onVisible = () => {
    if (!document.hidden) void pump()
  }
  document.addEventListener('visibilitychange', onVisible)

  return {
    push(frame) {
      next = frame
      void pump()
    },
    redraw() {
      if (!next && last) next = last
      void pump()
    },
    dispose() {
      gone = true
      next = last = null
      document.removeEventListener('visibilitychange', onVisible)
    },
  }
}
