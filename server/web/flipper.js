'use strict';

// Characters used for the random intermediate flips (uppercase + digits + punctuation)
const CHARS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZÅÄÖ0123456789 :.·-';

// Fixed display widths (characters) for each column.
// Strings are uppercased and padded/truncated to these widths.
const COLS = [
  { key: 'time',      width: 8,  label: 'Time'        },
  { key: 'direction', width: 24, label: 'Destination' },
  { key: 'line',      width: 5,  label: 'Line'        },
  { key: 'timeLeft',  width: 8,  label: 'In'          },
  { key: 'stop',      width: 20, label: 'Stop'        },
];

let refreshInterval = 120;
let refreshTimer    = null;

// ── helpers ───────────────────────────────────────────────────────────────────

function pad(str, width) {
  str = (str == null ? '' : String(str)).toUpperCase().slice(0, width);
  return str.padEnd(width, ' ');
}

function randomChar() {
  return CHARS[Math.floor(Math.random() * CHARS.length)];
}

// ── tile animation ────────────────────────────────────────────────────────────

function setChar(tile, ch) {
  tile.textContent = ch === ' ' ? '\u00A0' : ch;
  tile.dataset.char = ch;
  tile.classList.toggle('tile-space', ch === ' ');
}

// Animate a tile through 2–4 random characters before settling on targetChar.
// initialDelay staggers the start so characters flip left-to-right.
function flipToChar(tile, targetChar, initialDelay) {
  if (tile.dataset.char === targetChar) return;

  const cycles = 2 + Math.floor(Math.random() * 3); // 2–4 random flips
  let cycle = 0;

  function doFlip() {
    // Remove and re-add the class to restart the CSS animation.
    tile.classList.remove('flip');
    void tile.offsetWidth; // force reflow
    tile.classList.add('flip');

    // Change content at the animation midpoint (tile is edge-on / invisible).
    setTimeout(function () {
      var ch = cycle < cycles ? randomChar() : targetChar;
      setChar(tile, ch);

      if (cycle < cycles) {
        cycle++;
        setTimeout(doFlip, 80); // wait for the current animation to finish
      }
    }, 70); // ≈ midpoint of the 140 ms animation
  }

  setTimeout(doFlip, initialDelay);
}

// ── board rendering ───────────────────────────────────────────────────────────

// Build the static column-label header row.
function buildHeader() {
  var row = document.createElement('div');
  row.className = 'header-row';

  COLS.forEach(function (col) {
    var cell = document.createElement('div');
    cell.className = 'header-cell';
    // Match the data cell width exactly: N tiles × 1.3ch + (N-1) × 1px gap between tiles
    cell.style.width = 'calc(' + col.width + ' * 1.3ch + ' + (col.width - 1) + 'px)';
    var label = document.createElement('span');
    label.className = 'header-label';
    label.textContent = col.label;
    cell.appendChild(label);
    row.appendChild(cell);
  });

  return row;
}

// Build a fresh departure row; all tiles animate in from blank.
function buildRow(dep, rowIndex) {
  var row = document.createElement('div');
  row.className = 'dep-row';

  var charOffset = 0;

  COLS.forEach(function (col) {
    var cell = document.createElement('div');
    cell.className = 'dep-cell cell-' + col.key;

    let depVal = dep[col.key];
    console.log('Updating', col.key, 'to', depVal);

    if (col.key === 'timeLeft' && typeof depVal === 'number') {
      depVal = depVal <= 0 ? 'Now' : depVal + ' min';
    }

    var text = pad(depVal, col.width);

    for (var i = 0; i < col.width; i++) {
      var tile = document.createElement('span');
      tile.className = 'tile tile-space';
      tile.dataset.char = ' ';
      tile.textContent = '\u00A0';
      cell.appendChild(tile);

      // Stagger: row offset + per-character offset within the whole row
      var delay = rowIndex * 80 + charOffset * 20;
      flipToChar(tile, text[i], delay);
      charOffset++;
    }

    row.appendChild(cell);
  });

  return row;
}

// Update an existing row in-place, animating only changed characters.
function updateRow(row, dep) {
  var charOffset = 0;

  COLS.forEach(function (col) {
    var cell = row.querySelector('.cell-' + col.key);
    if (!cell) return;

    let depVal = dep[col.key];
    console.log('Updating', col.key, 'to', depVal);
    if (col.key === 'timeLeft' && typeof depVal === 'number') {
      depVal = depVal <= 0 ? 'Now' : depVal + ' min';
    }

    var tiles = cell.querySelectorAll('.tile');
    var text  = pad(depVal, col.width);

    for (var i = 0; i < col.width; i++) {
      if (tiles[i]) {
        flipToChar(tiles[i], text[i], charOffset * 15);
      }
      charOffset++;
    }
  });
}

function renderBoard(departures) {
  var board = document.getElementById('board');
  var rows  = board.querySelectorAll('.dep-row');

  if (rows.length !== departures.length) {
    // Row count changed — rebuild the whole board with entrance animation.
    board.innerHTML = '';
    board.appendChild(buildHeader());
    departures.forEach(function (dep, i) {
      board.appendChild(buildRow(dep, i));
    });
    return;
  }

  // Same number of rows — ensure header exists then update rows in-place.
  if (!board.querySelector('.header-row')) {
    board.insertBefore(buildHeader(), board.firstChild);
  }
  departures.forEach(function (dep, i) {
    updateRow(rows[i], dep);
  });
}

// ── clock ─────────────────────────────────────────────────────────────────────

function startClock() {
  var el = document.getElementById('flipper-clock');
  if (!el) return;
  function tick() {
    el.textContent = new Date().toLocaleTimeString('sv-SE', {
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
  }
  tick();
  setInterval(tick, 1000);
}

// ── data fetching ─────────────────────────────────────────────────────────────

function refresh() {
  clearTimeout(refreshTimer);
  var btn = document.getElementById('refresh-btn');
  if (btn) btn.classList.add('spinning');

  fetch('/api/flipper')
    .then(function (res) {
      if (!res.ok) throw new Error('HTTP ' + res.status);
      return res.json();
    })
    .then(function (data) {
      refreshInterval = data.refresh_interval || refreshInterval;
      renderBoard(data.departures || []);
    })
    .catch(function (err) {
      console.error('Flipper fetch failed:', err.message);
    })
    .finally(function () {
      if (btn) btn.classList.remove('spinning');
      refreshTimer = setTimeout(refresh, refreshInterval * 1000);
    });
}

// ── init ──────────────────────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', function () {
  startClock();
  var btn = document.getElementById('refresh-btn');
  if (btn) btn.addEventListener('click', refresh);
  refresh();
});
