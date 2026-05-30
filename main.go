package main

import (
	"embed"
	"log"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var trayIcon []byte

func main() {
	app := NewApp()
	app.loadSettings()

	go startSystemTray(app)

	err := wails.Run(&options.App{
		Title:             "Hello File Watcher",
		Width:             1100,
		Height:            800,
		StartHidden:       app.settings.RunInTrayOnStartup,
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}

func startSystemTray(app *App) {
	systray.Run(func() {
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
