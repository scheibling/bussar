// Departure Board — app.js
'use strict';

const ICONS = {
  bus:   '\u{1F68C}',
  tram:  '\u{1F68B}',
  train: '\u{1F686}',
  metro: '\u{1F687}',
  ferry: '\u26F4',
};

let refreshInterval = 120;
let refreshTimer    = null;
let countdownTimer  = null;
let lastData        = null;

// ── Theme ─────────────────────────────────────────────────────────────────────
const THEMES = ['theme-led', 'theme-flipboard'];
const THEME_LABELS = { 'theme-led': 'Flipboard', 'theme-flipboard': 'LED' };

function initTheme() {
  const saved = localStorage.getItem('bussar-theme') || 'theme-led';
  applyTheme(saved);
  const btn = document.getElementById('theme-toggle');
  if (btn) btn.addEventListener('click', toggleTheme);
}

function applyTheme(theme) {
  const html = document.documentElement;
  THEMES.forEach(function(t) { html.classList.remove(t); });
  html.classList.add(theme);
  localStorage.setItem('bussar-theme', theme);
  const btn = document.getElementById('theme-toggle');
  if (btn) btn.textContent = THEME_LABELS[theme] || 'Switch';
}

function toggleTheme() {
  const html = document.documentElement;
  const current = THEMES.find(function(t) { return html.classList.contains(t); }) || THEMES[0];
  const next = THEMES[(THEMES.indexOf(current) + 1) % THEMES.length];
  applyTheme(next);
}

// ── Bootstrap ────────────────────────────────────────────────────────────────
async function init() {
  initTheme();
  startClock();
  await loadConfig();
  await refresh();
}

async function loadConfig() {
  try {
    const res = await fetch('/api/config');
    if (!res.ok) return;
    const cfg = await res.json();
    refreshInterval = cfg.refresh_interval ?? 120;
  } catch (_) { /* use default */ }
}

function scheduleRefresh() {
  clearTimeout(refreshTimer);
  refreshTimer = setTimeout(refresh, refreshInterval * 1000);
}

function scheduleCountdown() {
  clearInterval(countdownTimer);
  countdownTimer = setInterval(updateCountdowns, 30_000);
}

async function refresh() {
  try {
    const res = await fetch('/api/departures');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    lastData = await res.json();
    renderBoard(lastData);
    setStatus('Updated ' + formatTime(new Date(lastData.generated_at)));
    hideError();
  } catch (err) {
    showError('Fetch failed: ' + err.message + '. Showing stale data.');
    if (lastData) renderBoard(lastData);
  }
  scheduleRefresh();
  scheduleCountdown();
}

// ── Clock ────────────────────────────────────────────────────────────────────
function startClock() {
  const el = document.getElementById('clock');
  function tick() {
    el.textContent = new Date().toLocaleTimeString('sv-SE', {
      hour: '2-digit', minute: '2-digit', second: '2-digit'
    });
  }
  tick();
  setInterval(tick, 1000);
}

// ── Render board ─────────────────────────────────────────────────────────────
function renderBoard(data) {
  const board = document.getElementById('board');
  board.innerHTML = '';
  for (const panel of data.panels ?? []) {
    board.appendChild(buildPanel(panel));
  }
}

function buildPanel(panel) {
  const div = document.createElement('div');
  div.className = 'panel ' + (panel.mode ?? 'combined');
  div.dataset.panelName = panel.name;

  const h = document.createElement('div');
  h.className = 'panel-header';
  h.textContent = panel.name;
  div.appendChild(h);

  if (panel.mode === 'separate') {
    const wrap = document.createElement('div');
    wrap.className = 'columns-wrap';
    for (const col of panel.columns ?? []) {
      wrap.appendChild(buildColumn(col));
    }
    div.appendChild(wrap);
  } else {
    div.appendChild(buildTable(panel.departures ?? []));
  }

  return div;
}

function buildColumn(col) {
  const div = document.createElement('div');
  div.className = 'column';

  const h = document.createElement('div');
  h.className = 'column-header';
  h.textContent = col.stop_name;
  div.appendChild(h);

  div.appendChild(buildTable(col.departures ?? []));
  return div;
}

function buildTable(departures) {
  const table = document.createElement('table');
  table.className = 'dep-table';

  const thead = table.createTHead();
  const hr = thead.insertRow();
  ['', 'Line', 'Destination', 'Time', 'Min'].forEach(function(t) {
    const th = document.createElement('th');
    th.textContent = t;
    if (t === 'Time') th.className = 'th-time';
    if (t === 'Min')  th.className = 'th-count';
    hr.appendChild(th);
  });

  const tbody = table.createTBody();
  if (departures.length === 0) {
    const row = tbody.insertRow();
    row.className = 'empty-row';
    const td = row.insertCell();
    td.colSpan = 5;
    td.textContent = 'No departures';
    return table;
  }

  const now = new Date();
  departures.forEach(function(dep, idx) {
    const row = buildRow(dep, now);
    row.style.animationDelay = (idx * 30) + 'ms';
    tbody.appendChild(row);
  });

  return table;
}

function buildRow(dep, now) {
  const row = document.createElement('tr');
  row.className = 'dep-row fade-in';
  row.dataset.scheduled = dep.scheduled;
  row.dataset.realtime  = dep.realtime ?? '';

  // icon
  const tdIcon = row.insertCell();
  tdIcon.className = 'td-icon';
  const icon = document.createElement('span');
  icon.className = 'transport-icon';
  icon.textContent = ICONS[dep.transport_type] || ICONS.bus;
  tdIcon.appendChild(icon);

  // line badge
  const tdLine = row.insertCell();
  tdLine.className = 'td-line';
  const badge = document.createElement('span');
  badge.className = 'line-badge';
  badge.textContent = dep.line;
  tdLine.appendChild(badge);

  // direction
  const tdDir = row.insertCell();
  tdDir.className = 'td-direction';
  tdDir.title = dep.direction;
  tdDir.textContent = dep.direction;

  // time
  const tdTime = row.insertCell();
  tdTime.className = 'td-time';
  tdTime.innerHTML = buildTimeHTML(dep);

  // countdown
  const tdCount = row.insertCell();
  tdCount.className = 'td-count';
  tdCount.innerHTML = buildCountdownHTML(dep, now);

  return row;
}

function buildTimeHTML(dep) {
  if (dep.cancelled) {
    return '<span class="sched-time" style="text-decoration:line-through;color:var(--red)">' + esc(dep.scheduled) + '</span>';
  }
  if (dep.realtime && dep.realtime !== dep.scheduled) {
    const schedMins = hmToMins(dep.scheduled);
    const rtMins    = hmToMins(dep.realtime);
    const rtClass   = rtMins > schedMins ? 'rt-time rt-late' : 'rt-time rt-early';
    return '<span class="sched-time">' + esc(dep.scheduled) + '</span> <span class="' + rtClass + '">' + esc(dep.realtime) + '</span>';
  }
  return '<span class="sched-time">' + esc(dep.scheduled) + '</span>';
}

function buildCountdownHTML(dep, now) {
  if (dep.cancelled) return '<span class="countdown-cancelled">CANCELLED</span>';
  return countdownSpan(computeCountdown(dep, now));
}

function countdownSpan(mins) {
  if (mins <= 0) return '<span class="countdown-now">NOW</span>';
  if (mins <= 1) return '<span class="countdown-now">' + mins + '&nbsp;min</span>';
  if (mins <= 2) return '<span class="countdown-boarding">' + mins + '&nbsp;min</span>';
  if (mins <= 5) return '<span class="countdown-soon">' + mins + '&nbsp;min</span>';
  return '<span class="countdown-normal">' + mins + '&nbsp;min</span>';
}

function hmToMins(hm) {
  const parts = hm.split(':');
  return parseInt(parts[0], 10) * 60 + parseInt(parts[1], 10);
}

function computeCountdown(dep, now) {
  const timeStr = (dep.realtime && dep.realtime !== dep.scheduled) ? dep.realtime : dep.scheduled;
  const today = now.toLocaleDateString('sv-SE'); // YYYY-MM-DD
  const dt = new Date(today + 'T' + timeStr + ':00');
  // Handle wrap past midnight: if computed time is > 2 h in the past, add a day.
  if (dt < now && (now - dt) > 2 * 3600000) dt.setDate(dt.getDate() + 1);
  return Math.max(0, Math.round((dt - now) / 60000));
}

// ── Live countdown updates (every 30 s, no network) ──────────────────────────
function updateCountdowns() {
  const now = new Date();
  document.querySelectorAll('.dep-row').forEach(function(row) {
    if (!row.dataset.scheduled) return;
    const fake = { scheduled: row.dataset.scheduled, realtime: row.dataset.realtime || '' };
    const mins = computeCountdown(fake, now);
    const tdCount = row.cells[4];
    if (tdCount) tdCount.innerHTML = countdownSpan(mins);
  });
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function esc(str) {
  return String(str ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function formatTime(d) {
  if (!(d instanceof Date) || isNaN(d)) return '—';
  return d.toLocaleTimeString('sv-SE', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function setStatus(msg) {
  const el = document.getElementById('status');
  if (el) el.textContent = msg;
}

function showError(msg) {
  const el = document.getElementById('error-banner');
  if (el) { el.textContent = msg; el.classList.remove('hidden'); }
}

function hideError() {
  const el = document.getElementById('error-banner');
  if (el) el.classList.add('hidden');
}

// ── Start ─────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', init);
