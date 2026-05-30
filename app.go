package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type WatchEntry struct {
	ID       int    `json:"id"`
	Path     string `json:"path"`
	Dir      string `json:"dir"`
	Enabled  bool   `json:"enabled"`
	LastHash string `json:"lastHash"`
}

type HashEvent struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Hash      string `json:"hash"`
	Timestamp string `json:"timestamp"`
}

type App struct {
	ctx context.Context

	mu       sync.Mutex
	nextID   int
	watches  map[int]*WatchEntry
	watchers map[int]*fsnotify.Watcher
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

	hash, err := hashFile(absPath)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	id := a.nextID
	a.nextID++

	entry := &WatchEntry{
		ID:       id,
		Path:     absPath,
		Dir:      filepath.Dir(absPath),
		Enabled:  true,
		LastHash: hash,
	}

	a.watches[id] = entry
	a.mu.Unlock()

	if err := a.startWatcher(id); err != nil {
		return nil, err
	}

	a.emitHash(entry, hash)
	return entry, nil
}

func (a *App) GetWatches() []*WatchEntry {
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
		return a.startWatcher(id)
	}

	a.stopWatcher(id)
	return nil
}

func (a *App) RemoveWatch(id int) error {
	a.stopWatcher(id)

	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.watches, id)
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

		a.mu.Lock()
		entry, ok := a.watches[id]
		if !ok || !entry.Enabled {
			a.mu.Unlock()
			return
		}

		if entry.LastHash == hash {
			a.mu.Unlock()
			return
		}

		entry.LastHash = hash
		copy := *entry
		a.mu.Unlock()

		a.emitHash(&copy, hash)
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
		ID:        entry.ID,
		Path:      entry.Path,
		Hash:      hash,
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
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
