package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Raven",
		Width:     980,
		Height:    720,
		MinWidth:  720,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 247, G: 246, B: 243, A: 1}, // Parchment paper
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
			// Stop the webview from opening a dropped file as an "untitled tab"
			// (notably on Windows WebView2). Wails' OnFileDrop still fires.
			DisableWebViewDrop: true,
		},
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                  true,
				FullSizeContent:            true,
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
