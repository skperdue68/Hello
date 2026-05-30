import './style.css';

import {
  SelectAndAddFile,
  GetWatches,
  SetEnabled,
  RemoveWatch,
  GetAPIKey,
  SaveAPIKey,
  GetSocketURL,
  GetRemoteChangeLog,
  GetRunInTrayOnStartup,
  SetRunInTrayOnStartup,
  HideToTray,
  QuitApp
} from '../wailsjs/go/main/App';

import { EventsOn } from '../wailsjs/runtime/runtime';
import { io } from 'socket.io-client';

const app = document.querySelector('#app');

let watches = [];
let logLines = [];
let socket = null;

async function connectSocket() {
  const socketURL = await GetSocketURL();
  socket = io(socketURL);

  socket.on('connect', () => {
    console.log(`Connected to API websocket: ${socketURL}`);
  });

  socket.on('remote-file-updated', (payload) => {
    for (const watch of watches) {
      if (watch.fileName === payload.fileName) {
        watch.remoteHash = payload.sha256;
        watch.lastUploadedBy = payload.lastUploadedBy;
        watch.lastUploadedAt = payload.lastUploadedAt;
      }
    }

    logLines.unshift({
      timestamp: new Date().toLocaleString(),
      path: `REMOTE: ${payload.fileName}`,
      hash: `Remote Updated: ${payload.sha256} by ${payload.lastUploadedBy} at ${formatDate(payload.lastUploadedAt)}`
    });

    renderWatches();
    renderLog();
  });
}

app.innerHTML = `
  <main>
    <section class="header">
      <div>
        <h1>Hello File Watcher</h1>
        <p>Watch files, compare SHA-256 values, and upload changed files.</p>
      </div>
      <button id="addFileButton">Add File</button>
    </section>

    <section class="panel">
      <h2>API Key</h2>
      <div class="api-key-row">
        <input id="apiKeyInput" type="password" placeholder="Enter API key">
        <button id="saveApiKeyButton">Save API Key</button>
        <span id="apiKeyStatus"></span>
      </div>
    </section>

    <section class="panel">
      <h2>Startup / Tray</h2>
      <div class="tray-row">
        <label>
          <input id="runInTrayCheckbox" type="checkbox">
          Start hidden when the application opens
        </label>
        <button id="hideToTrayButton">Hide Window</button>
        <button id="quitButton" class="danger">Quit</button>
      </div>
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
const apiKeyInput = document.querySelector('#apiKeyInput');
const saveApiKeyButton = document.querySelector('#saveApiKeyButton');
const apiKeyStatus = document.querySelector('#apiKeyStatus');
const runInTrayCheckbox = document.querySelector('#runInTrayCheckbox');
const hideToTrayButton = document.querySelector('#hideToTrayButton');
const quitButton = document.querySelector('#quitButton');
const watchList = document.querySelector('#watchList');
const hashLog = document.querySelector('#hashLog');

addFileButton.addEventListener('click', async () => {
  const entry = await SelectAndAddFile();
  if (entry) {
    watches = await GetWatches();
    renderWatches();
  }
});

saveApiKeyButton.addEventListener('click', async () => {
  try {
    await SaveAPIKey(apiKeyInput.value.trim());
    apiKeyStatus.textContent = 'API key saved.';
    apiKeyStatus.className = 'ok';
  } catch (err) {
    apiKeyStatus.textContent = String(err);
    apiKeyStatus.className = 'bad';
  }
});

runInTrayCheckbox.addEventListener('change', async () => {
  await SetRunInTrayOnStartup(runInTrayCheckbox.checked);
});

hideToTrayButton.addEventListener('click', async () => {
  await HideToTray();
});

quitButton.addEventListener('click', async () => {
  await QuitApp();
});

EventsOn('hash-changed', (event) => {
  logLines.unshift({
    timestamp: event.timestamp,
    path: event.path,
    hash: `Local: ${event.hash} | Remote Before Upload: ${event.remoteHash || 'none'}`
  });

  const watch = watches.find((item) => item.id === event.id);
  if (watch) {
    watch.lastHash = event.hash;
    watch.remoteHash = event.remoteHash;
    watch.lastUploadedBy = event.lastUploadedBy;
    watch.lastUploadedAt = event.lastUploadedAt;
  }

  renderWatches();
  renderLog();
});

EventsOn('remote-file-updated', (event) => {
  for (const watch of watches) {
    if (watch.fileName === event.fileName) {
      watch.remoteHash = event.sha256;
      watch.lastUploadedBy = event.lastUploadedBy;
      watch.lastUploadedAt = event.lastUploadedAt;
    }
  }

  logLines.unshift({
    timestamp: new Date().toLocaleString(),
    path: `UPLOAD: ${event.fileName}`,
    hash: `Remote Updated: ${event.sha256} by ${event.lastUploadedBy} at ${formatDate(event.lastUploadedAt)}`
  });

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

async function loadInitialData() {
  await connectSocket();

  apiKeyInput.value = await GetAPIKey();
  runInTrayCheckbox.checked = await GetRunInTrayOnStartup();

  await loadRemoteChangeLog();

  watches = await GetWatches();

  if (apiKeyInput.value) {
    apiKeyStatus.textContent = 'API key loaded.';
    apiKeyStatus.className = 'ok';
  }

  renderWatches();
  renderLog();
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
        <code>Local: ${escapeHtml(item.lastHash || 'No local hash yet')}</code>
        <code>Remote: ${escapeHtml(item.remoteHash || 'No remote hash yet')}</code>
        <span>Last uploaded by: <strong>${escapeHtml(item.lastUploadedBy || 'Unknown')}</strong></span>
        <span>Last uploaded at: <strong>${escapeHtml(formatDate(item.lastUploadedAt) || 'Unknown')}</strong></span>
        <span class="status ${item.enabled ? 'on' : 'off'}">
          ${item.enabled ? 'Watching' : 'Off'}
        </span>
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

function formatDate(value) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

async function loadRemoteChangeLog() {
  const entries = await GetRemoteChangeLog();

  logLines = entries.map((entry) => ({
    timestamp: formatDate(entry.lastUploadedAt),
    path: `REMOTE: ${entry.fileName}`,
    hash: `Remote Updated: ${entry.sha256} by ${entry.lastUploadedBy} at ${formatDate(entry.lastUploadedAt)}`
  }));
}

function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;');
}

loadInitialData();