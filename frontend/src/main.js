import './style.css';

import {
  SelectAndAddFile,
  GetWatches,
  SetEnabled,
  RemoveWatch
} from '../wailsjs/go/main/App';

import { EventsOn } from '../wailsjs/runtime/runtime';

const app = document.querySelector('#app');

let watches = [];
let logLines = [];

app.innerHTML = `
  <main>
    <section class="header">
      <div>
        <h1>Hello File Watcher</h1>
        <p>Watch files and log SHA-256 changes.</p>
      </div>
      <button id="addFileButton">Add File</button>
    </section>

    <section class="panel">
      <h2>Watched Files</h2>
      <div id="watchList" class="watch-list"></div>
    </section>

    <section class="panel">
      <h2>SHA-256 Change Log</h2>
      <div id="hashLog" class="hash-log"></div>
    </section>
  </main>
`;

const addFileButton = document.querySelector('#addFileButton');
const watchList = document.querySelector('#watchList');
const hashLog = document.querySelector('#hashLog');

addFileButton.addEventListener('click', async () => {
  const entry = await SelectAndAddFile();
  if (entry) {
    watches = await GetWatches();
    renderWatches();
  }
});

EventsOn('hash-changed', (event) => {
  logLines.unshift(event);

  const watch = watches.find((item) => item.id === event.id);
  if (watch) {
    watch.lastHash = event.hash;
  }

  renderWatches();
  renderLog();
});

EventsOn('watch-error', (message) => {
  logLines.unshift({
    timestamp: new Date().toLocaleString(),
    path: 'ERROR',
    hash: message
  });

  renderLog();
});

async function loadWatches() {
  watches = await GetWatches();
  renderWatches();
}

function renderWatches() {
  if (!watches.length) {
    watchList.innerHTML = `<p class="empty">No files are being watched.</p>`;
    return;
  }

  watchList.innerHTML = watches.map((item) => `
    <div class="watch-row">
      <div class="watch-info">
        <strong>${escapeHtml(fileName(item.path))}</strong>
        <span>${escapeHtml(item.path)}</span>
        <code>${escapeHtml(item.lastHash || 'No hash yet')}</code>
      </div>

      <div class="watch-actions">
        <button data-action="toggle" data-id="${item.id}">
          ${item.enabled ? 'Turn Off' : 'Turn On'}
        </button>
        <button class="danger" data-action="remove" data-id="${item.id}">
          Remove
        </button>
      </div>
    </div>
  `).join('');

  document.querySelectorAll('[data-action="toggle"]').forEach((button) => {
    button.addEventListener('click', async () => {
      const id = Number(button.dataset.id);
      const item = watches.find((watch) => watch.id === id);
      await SetEnabled(id, !item.enabled);
      watches = await GetWatches();
      renderWatches();
    });
  });

  document.querySelectorAll('[data-action="remove"]').forEach((button) => {
    button.addEventListener('click', async () => {
      const id = Number(button.dataset.id);
      await RemoveWatch(id);
      watches = await GetWatches();
      renderWatches();
    });
  });
}

function renderLog() {
  if (!logLines.length) {
    hashLog.innerHTML = `<p class="empty">No hash changes yet.</p>`;
    return;
  }

  hashLog.innerHTML = logLines.map((line) => `
    <div class="log-line">
      <div>
        <strong>${escapeHtml(line.timestamp)}</strong>
        <span>${escapeHtml(line.path)}</span>
      </div>
      <code>${escapeHtml(line.hash)}</code>
    </div>
  `).join('');
}

function fileName(path) {
  return path.split(/[\\/]/).pop();
}

function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;');
}

loadWatches();
renderLog();