'use strict';

// Service worker : c'est lui qui reçoit les notifications push.
//
// Le navigateur le réveille même quand l'application est fermée — c'est ce qui
// permet d'être alerté sans que rien ne tourne au premier plan. Le service de
// push (FCM sur Android) transporte une charge utile chiffrée que seul ce
// worker peut déchiffrer.
//
// Aucune mise en cache ici : les données du bot n'ont d'intérêt que fraîches, et
// servir un état périmé serait pire que ne rien afficher.

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));

self.addEventListener('push', event => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch {
    payload = { title: 'simple-bot', body: event.data ? event.data.text() : '' };
  }

  // userVisibleOnly impose d'afficher quelque chose : sans notification visible,
  // le navigateur finirait par révoquer l'abonnement.
  const title = payload.title || 'simple-bot';
  const options = {
    body: payload.body || '',
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    // Le tag regroupe : une nouvelle alerte d'erreur remplace la précédente
    // plutôt que d'empiler des doublons dans le tiroir.
    tag: payload.tag || 'simple-bot',
    renotify: payload.level === 'error',
    requireInteraction: payload.level === 'error',
    data: { kind: payload.kind, instance: payload.instance },
  };

  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', event => {
  event.notification.close();

  // Reprendre l'onglet existant s'il y en a un, plutôt qu'en ouvrir un second.
  event.waitUntil((async () => {
    const clients = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const client of clients) {
      if ('focus' in client) {
        client.postMessage('show-events');
        return client.focus();
      }
    }
    return self.clients.openWindow('/');
  })());
});
