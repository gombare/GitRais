// Package app è l'unico punto di contatto tra Wails e il backend.
package app

import "context"

type App struct {
	ctx context.Context
}

func New() *App { return &App{} }

func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

func (a *App) Shutdown(ctx context.Context) {}

func (a *App) Version() string { return "0.1.0" }
