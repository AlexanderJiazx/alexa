// Service worker: shows Alexa's push notifications and, on click, focuses
// the app on what they're about (a report, or the conversation).
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));
self.addEventListener('push', (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch { data = { title: 'Alexa', body: event.data && event.data.text() }; }
  event.waitUntil(self.registration.showNotification(data.title || 'Alexa', {
    body: data.body || '',
    icon: '/icon.svg',
    badge: '/icon.svg',
    // An updated report replaces its earlier notification.
    tag: data.ref || undefined,
    data: { ref: data.ref || '' },
  }));
});
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const ref = (event.notification.data && event.notification.data.ref) || '';
  const url = ref.startsWith('report:') ? '/?report=' + encodeURIComponent(ref.slice(7)) : '/';
  event.waitUntil((async () => {
    const all = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const c of all) {
      if ('focus' in c) { c.postMessage({ type: 'open', ref }); return c.focus(); }
    }
    return self.clients.openWindow(url);
  })());
});
