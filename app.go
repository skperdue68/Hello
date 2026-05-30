package main

import (
	"context"
	"time"
)

type App struct {
	ctx context.Context
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) GetHelloTime() string {
	return "Hello " + time.Now().Format("Monday, January 2, 2006 3:04:05 PM")
}
