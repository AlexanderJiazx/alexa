import { api } from '../api/client'
import type { ServerConfig } from '../api/types'

function urlB64ToUint8Array(b64: string) {
  const pad = '='.repeat((4 - (b64.length % 4)) % 4)
  const raw = atob((b64 + pad).replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(raw, (c) => c.charCodeAt(0))
}

export async function enablePush(config: ServerConfig) {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) throw new Error('This browser doesn’t support push notifications (on iPhone, add Alexa to your home screen first).')
  const perm = await Notification.requestPermission()
  if (perm !== 'granted') throw new Error('Notification permission was not granted.')
  const reg = await navigator.serviceWorker.ready
  let sub = await reg.pushManager.getSubscription()
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: urlB64ToUint8Array(config.vapidPublicKey) })
  await api.pushSubscribe(sub.toJSON())
}
