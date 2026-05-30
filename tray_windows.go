//go:build windows

package main

import (
	_ "embed"
	"log"

	"github.com/getlantern/systray"
)

//go:embed build/appicon.ico
var trayIcon []byte

func startSystemTray(app *App) {
	systray.Run(func() {
		log.Printf("Tray icon bytes: %d", len(trayIcon))

		systray.SetIcon(trayIcon)
		systray.SetTitle("Hello File Watcher")
		systray.SetTooltip("Hello File Watcher")

		showItem := systray.AddMenuItem("Show Window", "Show the application window")
		hideItem := systray.AddMenuItem("Hide Window", "Hide the application window")
		systray.AddSeparator()
		quitItem := systray.AddMenuItem("Quit", "Quit Hello File Watcher")

		go func() {
			for {
				select {
				case <-showItem.ClickedCh:
					app.ShowFromTray()

				case <-hideItem.ClickedCh:
					app.HideToTray()

				case <-quitItem.ClickedCh:
					systray.Quit()
					app.QuitApp()
					return
				}
			}
		}()
	}, func() {
		// Tray cleanup if needed later.
	})
}
