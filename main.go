package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"gitrais/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	a := app.New()
	err := wails.Run(&options.App{
		Title:       "Gitrais",
		Width:       1400,
		Height:      900,
		MinWidth:    900,
		MinHeight:   600,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   a.Startup,
		OnShutdown:  a.Shutdown,
		Bind:        []interface{}{a},
	})
	if err != nil {
		log.Fatal(err)
	}
}
