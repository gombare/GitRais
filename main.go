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
	// The Edit menu is required for copy/paste inside the WebView. Repository
	// commands live in the toolbar and in keyboard shortcuts handled by the
	// frontend, so menu and shortcuts cannot disagree.
	appMenu := menu.NewMenu()
	appMenu.Append(menu.AppMenu())
	appMenu.Append(menu.EditMenu())
	help := appMenu.AddSubmenu("Aiuto")
	help.AddText("Activity log", nil, func(*menu.CallbackData) { _ = a.OpenActivityLog() })

	err := wails.Run(&options.App{
		Title:       "Gitrais",
		Width:       1400,
		Height:      900,
		MinWidth:    900,
		MinHeight:   600,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   func(ctx context.Context) { app.Startup(a, ctx) },
		OnShutdown:  func(ctx context.Context) { app.Shutdown(a, ctx) },
		Menu:        appMenu,
		Bind:        []interface{}{a},
	})
	if err != nil {
		log.Fatal(err)
	}
}
