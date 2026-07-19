'use strict';

// Application de supervision du bot.
//
// Elle ne parle qu'au relay : le bot lui-même est injoignable depuis internet.
// Une pause demandée ici est mise en file par le relay, puis récupérée par le
// bot lors de son prochain snapshot — d'où le libellé « demandée… » tant que
// l'acquittement n'est pas revenu.

const REFRESH_MS = 15000;
const TOKEN_KEY = 'relay-token';

let token = localStorage.getItem(TOKEN_KEY) || '';
let instance = null;
let lastSeq = 0;
let events = [];
let timer = null;

// ---------------------------------------------------------------- API

async function api(path, options = {}) {
  const resp = await fetch(path, {
    ...options,
    headers: {
      'Authorization': 'Bearer ' + token,
      'Content-Type': 'application/json',
      ...(options.headers || {}),
    },
  });
  if (resp.status === 401) {
    logout();
    throw new Error('jeton refusé');
  }
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({}));
    throw new Error(body.error || ('HTTP ' + resp.status));
  }
  return resp.status === 204 ? null : resp.json();
}

// ---------------------------------------------------------------- Formatage

function fmtDuration(seconds) {
  if (!seconds || seconds < 0) return '—';
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d} j ${h} h`;
  if (h > 0) return `${h} h ${m} min`;
  if (m > 0) return `${m} min`;
  return `${Math.floor(seconds)} s`;
}

function fmtTime(iso) {
  const date = new Date(iso);
  const today = new Date();
  const sameDay = date.toDateString() === today.toDateString();
  const time = date.toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' });
  return sameDay ? time : date.toLocaleDateString('fr-FR', { day: '2-digit', month: '2-digit' }) + ' ' + time;
}

const LEVEL_ICONS = { error: '⚠️', warn: '⚡', info: '•' };
const KIND_ICONS = {
  buy_filled: '🟢', sell_filled: '💰', pattern: '🔔',
  error: '⚠️', bot_silent: '📴',
};

// ---------------------------------------------------------------- Rendu

function renderStatus(view) {
  const snap = view.snapshot || {};
  const card = document.getElementById('status-card');

  // La couleur seule ne dit rien : chaque état porte une icône et un libellé.
  // Les glyphes sont distincts de la pastille, qui porte déjà la couleur.
  let cls = 'good', icon = '✓', label = 'Actif';
  if (!view.online) {
    cls = 'critical'; icon = '⛔'; label = 'Hors ligne';
  } else if (snap.paused) {
    cls = 'warning'; icon = '⏸'; label = 'En pause';
  } else if (snap.error) {
    cls = 'warning'; icon = '⚠'; label = 'Erreurs récentes';
  }

  const sub = view.online
    ? 'Dernier contact il y a ' + fmtDuration(view.silent_for_s)
    : 'Silencieux depuis ' + fmtDuration(view.silent_for_s);

  card.innerHTML = `
    <div class="status ${cls}">
      <span class="dot"></span><span>${icon} ${label}</span>
      <span class="sub">${sub}</span>
    </div>
    ${snap.error ? `<p class="banner" style="margin:12px 0 0;font-size:14px">
      ${escapeHtml(snap.error.message)}
      <br><span style="color:var(--text-muted);font-size:12px">il y a ${fmtDuration(snap.error.ago_s)}</span>
    </p>` : ''}
  `;
}

function renderTiles(snap) {
  const portfolio = snap.portfolio;
  const tiles = [
    // Pas d'unité ici : la paire affichée en libellé porte déjà la devise.
    { label: 'Prix ' + (snap.pair || ''), value: snap.price || '—' },
    { label: 'RSI' + (snap.rsi_timeframe ? ` (${snap.rsi_timeframe})` : ''), value: snap.rsi || '—' },
    { label: 'Cycles actifs', value: snap.active_cycles ?? '—',
      note: (snap.open_cycles ?? 0) + ' achat(s) rempli(s)' },
    { label: 'Ordres ouverts', value: snap.open_orders ?? '—' },
  ];

  if (portfolio) {
    tiles.push({
      wide: true,
      label: 'Portefeuille',
      value: portfolio.total,
      unit: portfolio.quote,
      note: portfolio.locked ? `dont ${portfolio.locked} ${portfolio.quote} bloqués en ordres` : 'rien de bloqué',
    });
  }

  document.getElementById('tiles').innerHTML = tiles.map(t => `
    <div class="tile ${t.wide ? 'wide' : ''}">
      <div class="label">${escapeHtml(t.label)}</div>
      <div class="value">${escapeHtml(String(t.value))}${t.unit ? ` <span class="unit">${escapeHtml(t.unit)}</span>` : ''}</div>
      ${t.note ? `<div class="note">${escapeHtml(t.note)}</div>` : ''}
    </div>
  `).join('');
}

function renderDetails(view) {
  const snap = view.snapshot || {};
  const rows = [
    ['Instance', view.instance],
    ['Version', snap.version || '—'],
    ['Exchange', snap.exchange || '—'],
    ['Uptime', fmtDuration(snap.uptime_s)],
    ['Dernier price-check', 'il y a ' + fmtDuration(snap.last_check_ago_s)],
  ];
  document.getElementById('details').innerHTML = rows
    .map(([k, v]) => `<dt>${escapeHtml(k)}</dt><dd>${escapeHtml(String(v))}</dd>`).join('');
}

function renderActions(view) {
  const snap = view.snapshot || {};
  const btn = document.getElementById('toggle-pause');
  const note = document.getElementById('action-note');

  // Une commande non encore acquittée : le bot ne l'a pas forcément vue.
  const pending = (view.commands || []).find(c => !c.acked_at);
  if (pending) {
    btn.disabled = true;
    btn.textContent = pending.action === 'pause' ? 'Pause demandée…' : 'Reprise demandée…';
    note.textContent = 'En attente du prochain contact du bot (jusqu’à une minute).';
    return;
  }

  btn.disabled = !view.online;
  btn.textContent = snap.paused ? 'Reprendre' : 'Mettre en pause';
  btn.dataset.action = snap.paused ? 'resume' : 'pause';

  const last = (view.commands || [])[0];
  note.textContent = !view.online
    ? 'Bot injoignable : aucune commande ne peut être transmise.'
    : (last && last.ok === false ? 'Dernière commande refusée : ' + (last.error || '') : '');
}

function renderEvents() {
  const box = document.getElementById('events');
  if (events.length === 0) {
    box.innerHTML = '<p class="empty">Aucune notification.</p>';
    return;
  }
  box.innerHTML = events.slice().reverse().map(stored => {
    const e = stored.event;
    const icon = KIND_ICONS[e.kind] || LEVEL_ICONS[e.level] || '•';
    return `
      <div class="event ${e.level === 'error' ? 'error' : ''}">
        <div class="icon">${icon}</div>
        <div>
          <div class="title">${escapeHtml(e.title || e.kind)}</div>
          ${e.text ? `<div class="text">${escapeHtml(e.text)}</div>` : ''}
          <div class="when">${fmtTime(stored.received_at)}</div>
        </div>
      </div>`;
  }).join('');
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

// ---------------------------------------------------------------- Cycle de vie

async function refresh() {
  try {
    const state = await api('/api/state');
    const view = (state.instances || [])[0];
    if (!view) {
      document.getElementById('status-card').innerHTML =
        '<p class="empty">Aucune instance connue du relay.</p>';
      return;
    }

    instance = view.instance;
    document.getElementById('header-meta').textContent = view.instance;

    renderStatus(view);
    renderTiles(view.snapshot || {});
    renderDetails(view);
    renderActions(view);

    const fresh = await api(`/api/events?instance=${encodeURIComponent(instance)}&since=${lastSeq}`);
    if (fresh.events && fresh.events.length) {
      events = events.concat(fresh.events);
      lastSeq = fresh.events[fresh.events.length - 1].seq;
      renderEvents();
    }
  } catch (err) {
    console.error('rafraîchissement impossible', err);
  }
}

async function sendCommand(action) {
  const btn = document.getElementById('toggle-pause');
  btn.disabled = true;
  try {
    await api('/api/commands', {
      method: 'POST',
      body: JSON.stringify({ instance, action }),
    });
    await refresh();
  } catch (err) {
    document.getElementById('action-note').textContent = 'Échec : ' + err.message;
    btn.disabled = false;
  }
}

// ---------------------------------------------------------------- Web Push

function urlBase64ToUint8Array(base64) {
  const padded = (base64 + '='.repeat((4 - base64.length % 4) % 4))
    .replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(padded);
  return Uint8Array.from([...raw].map(c => c.charCodeAt(0)));
}

// readyWorker attend le service worker sans risquer d'attendre indéfiniment :
// serviceWorker.ready ne se résout jamais si l'enregistrement a échoué.
function readyWorker(timeoutMs = 3000) {
  return Promise.race([
    navigator.serviceWorker.ready,
    new Promise((_, reject) =>
      setTimeout(() => reject(new Error('service worker indisponible')), timeoutMs)),
  ]);
}

async function refreshPushButton() {
  const btn = document.getElementById('push-toggle');
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    btn.disabled = true;
    btn.textContent = 'Push non supporté';
    return;
  }

  let reg;
  try {
    reg = await readyWorker();
  } catch {
    btn.disabled = true;
    btn.textContent = 'Push indisponible';
    return;
  }

  const sub = await reg.pushManager.getSubscription();
  btn.textContent = sub ? 'Notifications activées' : 'Activer les notifications';
  btn.dataset.subscribed = sub ? '1' : '';
}

async function togglePush() {
  const btn = document.getElementById('push-toggle');
  const note = document.getElementById('action-note');
  btn.disabled = true;

  try {
    const reg = await readyWorker();
    const existing = await reg.pushManager.getSubscription();

    if (existing) {
      await api('/api/push/unsubscribe', {
        method: 'POST',
        body: JSON.stringify({ endpoint: existing.endpoint }),
      });
      await existing.unsubscribe();
    } else {
      // Sans permission, pushManager.subscribe échouerait de toute façon.
      const permission = await Notification.requestPermission();
      if (permission !== 'granted') {
        note.textContent = 'Notifications refusées par le navigateur.';
        return;
      }
      const { key } = await api('/api/push/key');
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(key),
      });
      await api('/api/push/subscribe', { method: 'POST', body: JSON.stringify(sub) });
      note.textContent = '';
    }
  } catch (err) {
    note.textContent = 'Push : ' + err.message;
  } finally {
    btn.disabled = false;
    await refreshPushButton();
  }
}

// ---------------------------------------------------------------- Navigation

function showView(name) {
  for (const view of ['login', 'state', 'events']) {
    document.getElementById('view-' + view).classList.toggle('hidden', view !== name);
  }
  document.querySelectorAll('#nav button').forEach(b => {
    if (b.dataset.view === name) b.setAttribute('aria-current', 'page');
    else b.removeAttribute('aria-current');
  });

  // L'ancre reflète la vue : le bouton « retour » d'Android revient à l'écran
  // précédent au lieu de quitter l'application.
  const hash = name === 'events' ? '#events' : '';
  if (name !== 'login' && location.hash !== hash) {
    history.pushState(null, '', hash || location.pathname);
  }
}

// Vue initiale : celle demandée par l'ancre, sinon l'état.
function viewFromHash() {
  return location.hash === '#events' ? 'events' : 'state';
}

window.addEventListener('popstate', () => {
  if (token) showView(viewFromHash());
});

function logout() {
  localStorage.removeItem(TOKEN_KEY);
  token = '';
  if (timer) clearInterval(timer);
  document.getElementById('nav').classList.add('hidden');
  showView('login');
}

async function start() {
  document.getElementById('nav').classList.remove('hidden');
  showView(viewFromHash());
  await refresh();
  await refreshPushButton();
  timer = setInterval(refresh, REFRESH_MS);
}

// ---------------------------------------------------------------- Démarrage

document.getElementById('login').addEventListener('click', async () => {
  const input = document.getElementById('token');
  const error = document.getElementById('login-error');
  token = input.value.trim();
  if (!token) return;

  try {
    await api('/api/state');
    localStorage.setItem(TOKEN_KEY, token);
    error.classList.add('hidden');
    await start();
  } catch (err) {
    error.textContent = 'Connexion refusée : ' + err.message;
    error.classList.remove('hidden');
  }
});

document.getElementById('toggle-pause').addEventListener('click', e => {
  sendCommand(e.target.dataset.action || 'pause');
});
document.getElementById('push-toggle').addEventListener('click', togglePush);
document.querySelectorAll('#nav button').forEach(b => {
  b.addEventListener('click', () => showView(b.dataset.view));
});

// Une notification cliquée demande l'ouverture directe du journal.
navigator.serviceWorker?.addEventListener('message', event => {
  if (event.data === 'show-events') showView('events');
});

(async () => {
  // Sans attendre : un enregistrement lent ou impossible coûte les notifications
  // push, il ne doit pas retarder — ni empêcher — l'affichage de l'état.
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js')
      .catch(err => console.error('service worker non enregistré', err));
  }
  if (token) await start();
  else showView('login');
})();
