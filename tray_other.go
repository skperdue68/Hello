//go:build !windows

package main

func startSystemTray(app *App) {
	// System tray disabled on macOS/Linux to avoid Wails AppDelegate conflicts.
}
