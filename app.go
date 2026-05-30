package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const apiBaseURL = "http://localhost:3001"

type WatchEntry struct {
	ID             int    `json:"id"`
	Path           string `json:"path"`
	Dir            string `json:"dir"`
	FileName       string `json:"fileName"`
	Enabled        bool   `json:"enabled"`
	LastHash       string `json:"lastHash"`
	RemoteHash     string `json:"remoteHash"`
	LastUploadedBy string `json:"lastUploadedBy"`
	LastUploadedAt string `json:"lastUploadedAt"`
}

type SavedWatchEntry struct {
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

type AppSettings struct {
	APIKey string `json:"apiKey"`
}

type HashEvent struct {
	ID             int    `json:"id"`
	Path           string `json:"path"`
	FileName       string `json:"fileName"`
	Hash           string `json:"hash"`
	RemoteHash     string `json:"remoteHash"`
	LastUploadedBy string `json:"lastUploadedBy"`
	LastUploadedAt string `json:"lastUploadedAt"`
	Timestamp      string `json:"timestamp"`
}

type App struct {
	ctx context.Context

	mu       sync.Mutex
	nextID   int
	watches  map[int]*WatchEntry
	watchers map[int]*fsnotify.Watcher
	settings AppSettings
}

func NewApp() *App {
	return &App{
		nextID:   1,
		watches:  make(map[int]*WatchEntry),
		watchers: make(map[int]*fsnotify.Watcher),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadSettings()
	a.loadSavedWatches()
}

func (a *App) GetAPIKey() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings.APIKey
}

func (a *App) SaveAPIKey(apiKey string) error {
	owner, err := validateAPIKey(apiKey)
	if err != nil {
		return err
	}

	if owner == "" {
		return fmt.Errorf("invalid API key")
	}

	a.mu.Lock()
	a.settings.APIKey = apiKey
	a.mu.Unlock()

	a.saveSettings()
	return nil
}

func (a *App) SelectAndAddFile() (*WatchEntry, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a file to watch",
	})
	if err != nil {
		return nil, err
	}

	if path == "" {
		return nil, nil
	}

	return a.AddFile(path)
}

func (a *App) AddFile(path string) (*WatchEntry, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return nil, fmt.Errorf("please select a file, not a directory")
	}

	fileName := filepath.Base(absPath)

	a.mu.Lock()
	for _, existing := range a.watches {
		if existing.Path == absPath {
			copy := *existing
			a.mu.Unlock()
			return &copy, nil
		}
	}
	a.mu.Unlock()

	remote, _ := getRemoteFileInfo(fileName)

	a.mu.Lock()
	id := a.nextID
	a.nextID++

	entry := &WatchEntry{
		ID:             id,
		Path:           absPath,
		Dir:            filepath.Dir(absPath),
		FileName:       fileName,
		Enabled:        true,
		LastHash:       "",
		RemoteHash:     remote.SHA256,
		LastUploadedBy: remote.LastUploadedBy,
		LastUploadedAt: remote.LastUploadedAt,
	}

	a.watches[id] = entry
	a.mu.Unlock()

	if err := a.startWatcher(id); err != nil {
		return nil, err
	}

	a.saveWatches()
	return entry, nil
}

func (a *App) GetWatches() []*WatchEntry {
	a.refreshRemoteInfo()

	a.mu.Lock()
	defer a.mu.Unlock()

	items := make([]*WatchEntry, 0, len(a.watches))
	for _, entry := range a.watches {
		copy := *entry
		items = append(items, &copy)
	}

	return items
}

func (a *App) SetEnabled(id int, enabled bool) error {
	a.mu.Lock()
	entry, ok := a.watches[id]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("watch entry not found")
	}

	entry.Enabled = enabled
	a.mu.Unlock()

	if enabled {
		if err := a.startWatcher(id); err != nil {
			return err
		}
	} else {
		a.stopWatcher(id)
	}

	a.saveWatches()
	return nil
}

func (a *App) RemoveWatch(id int) error {
	a.stopWatcher(id)

	a.mu.Lock()
	delete(a.watches, id)
	a.mu.Unlock()

	a.saveWatches()
	return nil
}

func (a *App) startWatcher(id int) error {
	a.mu.Lock()
	entry, ok := a.watches[id]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("watch entry not found")
	}

	if _, exists := a.watchers[id]; exists {
		a.mu.Unlock()
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		a.mu.Unlock()
		return err
	}

	if err := watcher.Add(entry.Dir); err != nil {
		watcher.Close()
		a.mu.Unlock()
		return err
	}

	a.watchers[id] = watcher
	path := entry.Path
	a.mu.Unlock()

	go a.watchLoop(id, path, watcher)
	return nil
}

func (a *App) stopWatcher(id int) {
	a.mu.Lock()
	watcher, ok := a.watchers[id]
	if ok {
		delete(a.watchers, id)
	}
	a.mu.Unlock()

	if ok {
		_ = watcher.Close()
	}
}

func (a *App) watchLoop(id int, watchedPath string, watcher *fsnotify.Watcher) {
	var debounceTimer *time.Timer
	var debounceChan <-chan time.Time

	triggerHash := func() {
		hash, err := hashFile(watchedPath)
		if err != nil {
			runtime.EventsEmit(a.ctx, "watch-error", fmt.Sprintf("%s: %v", watchedPath, err))
			return
		}

		fileName := filepath.Base(watchedPath)
		remote, _ := getRemoteFileInfo(fileName)

		a.mu.Lock()
		entry, ok := a.watches[id]
		apiKey := a.settings.APIKey

		if !ok || !entry.Enabled {
			a.mu.Unlock()
			return
		}

		entry.LastHash = hash
		entry.RemoteHash = remote.SHA256
		entry.LastUploadedBy = remote.LastUploadedBy
		entry.LastUploadedAt = remote.LastUploadedAt
		copy := *entry
		a.mu.Unlock()

		a.emitHash(&copy, hash)

		if hash == remote.SHA256 {
			return
		}

		owner, err := validateAPIKey(apiKey)
		if err != nil || owner == "" {
			runtime.EventsEmit(a.ctx, "watch-error", "Cannot upload: invalid or missing API key")
			return
		}

		uploaded, err := uploadFile(watchedPath, apiKey)
		if err != nil {
			runtime.EventsEmit(a.ctx, "watch-error", fmt.Sprintf("%s: %v", watchedPath, err))
			return
		}

		a.mu.Lock()
		if entry, ok := a.watches[id]; ok {
			entry.RemoteHash = uploaded.SHA256
			entry.LastUploadedBy = uploaded.LastUploadedBy
			entry.LastUploadedAt = uploaded.LastUploadedAt
			copy = *entry
		}
		a.mu.Unlock()

		runtime.EventsEmit(a.ctx, "remote-file-updated", uploaded)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if filepath.Clean(event.Name) == filepath.Clean(watchedPath) &&
				(event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename)) {
				if debounceTimer != nil {
					debounceTimer.Stop()
				}

				debounceTimer = time.NewTimer(500 * time.Millisecond)
				debounceChan = debounceTimer.C
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			runtime.EventsEmit(a.ctx, "watch-error", err.Error())

		case <-debounceChan:
			debounceChan = nil
			triggerHash()
		}
	}
}

func (a *App) emitHash(entry *WatchEntry, hash string) {
	runtime.EventsEmit(a.ctx, "hash-changed", HashEvent{
		ID:             entry.ID,
		Path:           entry.Path,
		FileName:       entry.FileName,
		Hash:           hash,
		RemoteHash:     entry.RemoteHash,
		LastUploadedBy: entry.LastUploadedBy,
		LastUploadedAt: entry.LastUploadedAt,
		Timestamp:      time.Now().Format("2006-01-02 15:04:05"),
	})
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()

	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func appConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "."
	}

	appDir := filepath.Join(configDir, "HelloFileWatcher")
	_ = os.MkdirAll(appDir, 0755)

	return appDir
}

func watchesConfigPath() string {
	return filepath.Join(appConfigDir(), "watches.json")
}

func settingsConfigPath() string {
	return filepath.Join(appConfigDir(), "settings.json")
}

func (a *App) saveSettings() {
	a.mu.Lock()
	settings := a.settings
	a.mu.Unlock()

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}

	_ = os.WriteFile(settingsConfigPath(), data, 0644)
}

func (a *App) loadSettings() {
	data, err := os.ReadFile(settingsConfigPath())
	if err != nil {
		return
	}

	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return
	}

	a.mu.Lock()
	a.settings = settings
	a.mu.Unlock()
}

func (a *App) saveWatches() {
	a.mu.Lock()
	defer a.mu.Unlock()

	saved := make([]SavedWatchEntry, 0, len(a.watches))

	for _, entry := range a.watches {
		saved = append(saved, SavedWatchEntry{
			Path:    entry.Path,
			Enabled: entry.Enabled,
		})
	}

	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return
	}

	_ = os.WriteFile(watchesConfigPath(), data, 0644)
}

func (a *App) loadSavedWatches() {
	data, err := os.ReadFile(watchesConfigPath())
	if err != nil {
		return
	}

	var saved []SavedWatchEntry
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}

	for _, item := range saved {
		absPath, err := filepath.Abs(item.Path)
		if err != nil {
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() {
			continue
		}

		fileName := filepath.Base(absPath)
		remote, _ := getRemoteFileInfo(fileName)

		a.mu.Lock()
		id := a.nextID
		a.nextID++

		entry := &WatchEntry{
			ID:             id,
			Path:           absPath,
			Dir:            filepath.Dir(absPath),
			FileName:       fileName,
			Enabled:        item.Enabled,
			LastHash:       "",
			RemoteHash:     remote.SHA256,
			LastUploadedBy: remote.LastUploadedBy,
			LastUploadedAt: remote.LastUploadedAt,
		}

		a.watches[id] = entry
		a.mu.Unlock()

		if item.Enabled {
			_ = a.startWatcher(id)
		}
	}
}

func (a *App) refreshRemoteInfo() {
	a.mu.Lock()
	entries := make([]*WatchEntry, 0, len(a.watches))
	for _, entry := range a.watches {
		copy := *entry
		entries = append(entries, &copy)
	}
	a.mu.Unlock()

	for _, entry := range entries {
		remote, err := getRemoteFileInfo(entry.FileName)
		if err != nil {
			continue
		}

		a.mu.Lock()
		if current, ok := a.watches[entry.ID]; ok {
			current.RemoteHash = remote.SHA256
			current.LastUploadedBy = remote.LastUploadedBy
			current.LastUploadedAt = remote.LastUploadedAt
		}
		a.mu.Unlock()
	}
}

type remoteFileInfoResponse struct {
	Exists         bool   `json:"exists"`
	FileName       string `json:"fileName"`
	SHA256         string `json:"sha256"`
	LastUploadedBy string `json:"lastUploadedBy"`
	LastUploadedAt string `json:"lastUploadedAt"`
}

type validateKeyResponse struct {
	Valid bool   `json:"valid"`
	Owner string `json:"owner"`
}

type uploadResponse struct {
	Success        bool   `json:"success"`
	FileName       string `json:"fileName"`
	SHA256         string `json:"sha256"`
	LastUploadedBy string `json:"lastUploadedBy"`
	LastUploadedAt string `json:"lastUploadedAt"`
	Error          string `json:"error"`
}

func validateAPIKey(apiKey string) (string, error) {
	if apiKey == "" {
		return "", nil
	}

	req, err := http.NewRequest("GET", apiBaseURL+"/api/validate-key", nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result validateKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if !result.Valid {
		return "", nil
	}

	return result.Owner, nil
}

func getRemoteFileInfo(fileName string) (remoteFileInfoResponse, error) {
	url := fmt.Sprintf("%s/api/file-info/%s", apiBaseURL, fileName)

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return remoteFileInfoResponse{}, err
	}
	defer resp.Body.Close()

	var result remoteFileInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return remoteFileInfoResponse{}, err
	}

	if !result.Exists {
		return remoteFileInfoResponse{FileName: fileName}, nil
	}

	return result, nil
}

func uploadFile(filePath string, apiKey string) (uploadResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	file, err := os.Open(filePath)
	if err != nil {
		return uploadResponse{}, err
	}
	defer file.Close()

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return uploadResponse{}, err
	}

	if _, err := io.Copy(part, file); err != nil {
		return uploadResponse{}, err
	}

	if err := writer.WriteField("fileName", filepath.Base(filePath)); err != nil {
		return uploadResponse{}, err
	}

	if err := writer.Close(); err != nil {
		return uploadResponse{}, err
	}

	req, err := http.NewRequest("POST", apiBaseURL+"/api/upload", &body)
	if err != nil {
		return uploadResponse{}, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return uploadResponse{}, err
	}
	defer resp.Body.Close()

	var result uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return uploadResponse{}, err
	}

	if !result.Success {
		if result.Error != "" {
			return uploadResponse{}, fmt.Errorf(result.Error)
		}
		return uploadResponse{}, fmt.Errorf("upload failed")
	}

	return result, nil
}
