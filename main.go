package main

import (
	"embed"
	"log"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	app.loadSettings()

	traySupported := goruntime.GOOS == "windows"

	if traySupported {
		go startSystemTray(app)
	}

	err := wails.Run(&options.App{
		Title:             "Hello File Watcher",
		Width:             1100,
		Height:            800,
		StartHidden:       traySupported && app.settings.RunInTrayOnStartup,
		HideWindowOnClose: traySupported,
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
