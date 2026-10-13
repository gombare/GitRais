package main

import (
	"context"
	"embed"
	"log"
	"runtime/debug"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"gitrais/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Soft heap limit: the graph index of a very large repository is the main
	// allocation, and without a limit the GC lets the heap grow to twice the
	// live set before collecting. 400 MiB keeps a 1M-commit repository well
	// within it while making the GC work harder only when memory is tight.
	debug.SetMemoryLimit(400 << 20)

	a := app.New()

	err := wails.Run(&options.App{
		Title:       "Gitrais",
		Width:       1400,
		Height:      900,
		MinWidth:    900,
		MinHeight:   600,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   func(ctx context.Context) { app.Startup(a, ctx) },
		OnShutdown:  func(ctx context.Context) { app.Shutdown(a, ctx) },
		Menu:        appMenu(func() { _ = a.OpenActivityLog() }),
		Bind:        []interface{}{a},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// appMenu builds the native menu bar. The Edit menu is required for copy/paste
// inside the WebView. Repository commands live in the toolbar and in keyboard
// shortcuts handled by the frontend, so menu and shortcuts cannot disagree.
func appMenu(openActivityLog func()) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	help := m.AddSubmenu("Aiuto")
	help.AddText("Log attività", nil, func(*menu.CallbackData) { openActivityLog() })
	return m
}
