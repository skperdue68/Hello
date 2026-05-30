const express = require('express');
const http = require('http');
const cors = require('cors');
const multer = require('multer');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { Server } = require('socket.io');

const PORT = 3001;

const STORAGE_DIR = path.join(__dirname, 'remote-files');
const UPLOAD_DIR = path.join(__dirname, 'uploads');
const DATA_DIR = path.join(__dirname, 'data');

const API_KEYS_FILE = path.join(DATA_DIR, 'api-keys.json');
const METADATA_FILE = path.join(DATA_DIR, 'file-metadata.json');

fs.mkdirSync(STORAGE_DIR, { recursive: true });
fs.mkdirSync(UPLOAD_DIR, { recursive: true });
fs.mkdirSync(DATA_DIR, { recursive: true });

const app = express();
const server = http.createServer(app);

const io = new Server(server, {
  cors: { origin: '*' }
});

app.use(cors());
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

const upload = multer({ dest: UPLOAD_DIR });

function loadJson(filePath, fallback) {
  try {
    if (!fs.existsSync(filePath)) return fallback;
    return JSON.parse(fs.readFileSync(filePath, 'utf8'));
  } catch {
    return fallback;
  }
}

function saveJson(filePath, data) {
  fs.writeFileSync(filePath, JSON.stringify(data, null, 2));
}

function loadApiKeys() {
  return loadJson(API_KEYS_FILE, {});
}

function saveApiKeys(keys) {
  saveJson(API_KEYS_FILE, keys);
}

function loadMetadata() {
  return loadJson(METADATA_FILE, {});
}

function saveMetadata(metadata) {
  saveJson(METADATA_FILE, metadata);
}

function safeFileName(fileName) {
  return path.basename(fileName);
}

function filePathFor(fileName) {
  return path.join(STORAGE_DIR, safeFileName(fileName));
}

function findApiKeyOwner(apiKey) {
  const keys = loadApiKeys();

  for (const [name, key] of Object.entries(keys)) {
    if (key === apiKey) {
      return name;
    }
  }

  return '';
}

function sha256File(filePath) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash('sha256');
    const stream = fs.createReadStream(filePath);

    stream.on('error', reject);
    stream.on('data', chunk => hash.update(chunk));
    stream.on('end', () => resolve(hash.digest('hex')));
  });
}

app.get('/api/validate-key', (req, res) => {
  const apiKey = req.header('x-api-key') || '';
  const owner = findApiKeyOwner(apiKey);

  res.json({
    valid: owner !== '',
    owner
  });
});

app.get('/api/file-info/:fileName', async (req, res) => {
  try {
    const fileName = safeFileName(req.params.fileName);
    const storedPath = filePathFor(fileName);
    const metadata = loadMetadata();

    if (!fs.existsSync(storedPath)) {
      return res.json({
        exists: false,
        fileName,
        sha256: '',
        lastUploadedBy: '',
        lastUploadedAt: ''
      });
    }

    const sha256 = await sha256File(storedPath);
    const info = metadata[fileName] || {};

    res.json({
      exists: true,
      fileName,
      sha256,
      lastUploadedBy: info.lastUploadedBy || '',
      lastUploadedAt: info.lastUploadedAt || ''
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

app.post('/api/upload', upload.single('file'), async (req, res) => {
  try {
    const apiKey = req.header('x-api-key') || '';
    const owner = findApiKeyOwner(apiKey);

    if (!owner) {
      if (req.file?.path && fs.existsSync(req.file.path)) {
        fs.unlinkSync(req.file.path);
      }

      return res.status(401).json({
        success: false,
        error: 'Invalid API key'
      });
    }

    const originalName = safeFileName(req.body.fileName || req.file.originalname);
    const targetPath = filePathFor(originalName);

    fs.copyFileSync(req.file.path, targetPath);
    fs.unlinkSync(req.file.path);

    const sha256 = await sha256File(targetPath);
    const lastUploadedAt = new Date().toISOString();

    const metadata = loadMetadata();
    metadata[originalName] = {
      fileName: originalName,
      sha256,
      lastUploadedBy: owner,
      lastUploadedAt
    };
    saveMetadata(metadata);

    const payload = {
      fileName: originalName,
      sha256,
      lastUploadedBy: owner,
      lastUploadedAt
    };

    io.emit('remote-file-updated', payload);

    res.json({
      success: true,
      ...payload
    });
  } catch (err) {
    res.status(500).json({ success: false, error: err.message });
  }
});

app.get('/admin', (req, res) => {
  const keys = loadApiKeys();

  const rows = Object.entries(keys).map(([name, key]) => `
    <tr>
      <td>${escapeHtml(name)}</td>
      <td><code>${escapeHtml(key)}</code></td>
      <td>
        <form method="post" action="/admin/keys/delete">
          <input type="hidden" name="name" value="${escapeHtml(name)}">
          <button type="submit">Remove</button>
        </form>
      </td>
    </tr>
  `).join('');

  res.send(`
<!doctype html>
<html>
<head>
  <title>API Key Admin</title>
  <style>
    body { font-family: Arial, sans-serif; margin: 2rem; background: #1e1e1e; color: white; }
    input, button { padding: .5rem; margin: .25rem; }
    table { border-collapse: collapse; width: 100%; margin-top: 1rem; }
    td, th { border: 1px solid #555; padding: .5rem; text-align: left; }
    code { color: #a8e6ff; }
  </style>
</head>
<body>
  <h1>API Key Admin</h1>

  <form method="post" action="/admin/keys">
    <input name="name" placeholder="Name" required>
    <input name="key" placeholder="API Key" required>
    <button type="submit">Add / Update Key</button>
  </form>

  <table>
    <thead>
      <tr>
        <th>Name</th>
        <th>API Key</th>
        <th>Action</th>
      </tr>
    </thead>
    <tbody>
      ${rows || '<tr><td colspan="3">No API keys yet.</td></tr>'}
    </tbody>
  </table>
</body>
</html>
  `);
});

app.post('/admin/keys', (req, res) => {
  const name = String(req.body.name || '').trim();
  const key = String(req.body.key || '').trim();

  if (name && key) {
    const keys = loadApiKeys();
    keys[name] = key;
    saveApiKeys(keys);
  }

  res.redirect('/admin');
});

app.post('/admin/keys/delete', (req, res) => {
  const name = String(req.body.name || '').trim();

  if (name) {
    const keys = loadApiKeys();
    delete keys[name];
    saveApiKeys(keys);
  }

  res.redirect('/admin');
});

io.on('connection', socket => {
  console.log(`Client connected: ${socket.id}`);

  socket.on('disconnect', () => {
    console.log(`Client disconnected: ${socket.id}`);
  });
});

function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;');
}

server.listen(PORT, () => {
  console.log(`API server running on http://localhost:${PORT}`);
  console.log(`Admin page: http://localhost:${PORT}/admin`);
});