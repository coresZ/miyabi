//go:build windows && miyabidesktop

// Package main is the Wails desktop shell for Miyabi. Wails owns the window,
// the single-instance lock and the lifecycle; the embedded Gin server owns the
// frontend, the API and SSE. The WebView is redirected to that server so every
// request shares a single origin.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/app"
	"github.com/ppxb/miyabi/internal/config"
	"github.com/ppxb/miyabi/internal/desktop"
	"github.com/ppxb/miyabi/internal/logging"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// singleInstanceID must stay stable across releases so a second launch is
// detected as the same application.
const singleInstanceID = "8f1f0f2e-6c9a-4d3e-9b3a-2f5c1d7a4b10"

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fatal(err)
	}

	logger, closeLog, err := newLogger(cfg)
	if err != nil {
		fatal(err)
	}
	defer closeLog()

	shell := &desktopApp{cfg: cfg, logger: logger, publicURL: desktop.LocalURL(cfg.Listen)}

	err = wails.Run(&options.App{
		Title:             "Miyabi",
		Width:             1280,
		Height:            800,
		MinWidth:          960,
		MinHeight:         600,
		HideWindowOnClose: false,
		AssetServer: &assetserver.Options{
			// The WebView is redirected to the embedded Gin server so that the
			// frontend, the API and SSE all share one origin. Streaming
			// responses (SSE, playback) are not supported through the Wails
			// asset server on Windows, so it must not proxy them.
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, shell.publicURL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			}),
		},
		OnStartup:     shell.startup,
		OnBeforeClose: shell.beforeClose,
		OnShutdown:    shell.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               singleInstanceID,
			OnSecondInstanceLaunch: shell.onSecondInstance,
		},
	})
	if err != nil {
		logger.Error("wails run failed", "error", err)
		os.Exit(1)
	}
}

type desktopApp struct {
	cfg       *config.Config
	logger    *slog.Logger
	publicURL string

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	backend *app.App
	done    chan struct{}
}

func (d *desktopApp) startup(ctx context.Context) {
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()

	if err := d.start(); err != nil {
		d.logger.Error("desktop backend failed to start", "error", err)
		_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:    runtime.ErrorDialog,
			Title:   "Miyabi 启动失败",
			Message: err.Error(),
		})
		runtime.Quit(ctx)
	}
}

func (d *desktopApp) start() error {
	listener, err := net.Listen("tcp", d.cfg.Listen)
	if err != nil {
		return fmt.Errorf("监听 %s 失败：%w", d.cfg.Listen, err)
	}
	backend, err := app.New(d.cfg, d.logger, app.WithDesktop(d))
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("初始化服务失败：%w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	d.mu.Lock()
	d.cancel = cancel
	d.backend = backend
	d.done = done
	d.mu.Unlock()

	go func() {
		defer close(done)
		if err := backend.RunListener(runCtx, listener); err != nil {
			d.logger.Error("backend stopped with error", "error", err)
		}
	}()
	return nil
}

func (d *desktopApp) beforeClose(context.Context) bool {
	// Closing the window quits the application; shutdown performs the graceful
	// stop so the database is closed cleanly.
	return false
}

func (d *desktopApp) shutdown(context.Context) {
	d.mu.Lock()
	cancel := d.cancel
	backend := d.backend
	done := d.done
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			d.logger.Warn("backend shutdown timed out")
		}
	}
	if backend != nil {
		if err := backend.Close(); err != nil {
			d.logger.Error("close backend", "error", err)
		}
	}
}

func (d *desktopApp) onSecondInstance(options.SecondInstanceData) {
	d.mu.Lock()
	ctx := d.ctx
	d.mu.Unlock()
	if ctx == nil {
		return
	}
	runtime.WindowShow(ctx)
	runtime.WindowUnminimise(ctx)
	runtime.WindowSetAlwaysOnTop(ctx, true)
	runtime.WindowSetAlwaysOnTop(ctx, false)
}

// RevealDataDir opens the portable data directory in Explorer.
func (d *desktopApp) RevealDataDir() error {
	return exec.Command("explorer", d.cfg.DataDir).Start()
}

// Quit asks the runtime to tear the application down.
func (d *desktopApp) Quit() {
	d.mu.Lock()
	ctx := d.ctx
	d.mu.Unlock()
	if ctx != nil {
		runtime.Quit(ctx)
	}
}

func loadConfig() (*config.Config, error) {
	if os.Getenv("MIYABI_LISTEN") == "" {
		_ = os.Setenv("MIYABI_LISTEN", desktop.DefaultListen)
	}
	if os.Getenv("MIYABI_DATA_DIR") == "" {
		dataDir, err := desktop.DefaultDataDir()
		if err != nil {
			return nil, err
		}
		_ = os.Setenv("MIYABI_DATA_DIR", dataDir)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cfg.Mode = config.RuntimeDesktop
	return &cfg, nil
}

func newLogger(cfg *config.Config) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, func() {}, err
	}
	file, err := os.OpenFile(filepath.Join(cfg.DataDir, "desktop.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, func() {}, err
	}
	logger, err := logging.New(file, cfg.LogLevel)
	if err != nil {
		_ = file.Close()
		return nil, func() {}, err
	}
	return logger, func() { _ = file.Close() }, nil
}

func fatal(err error) {
	_, _ = os.Stderr.WriteString("miyabi-desktop: " + err.Error() + "\n")
	os.Exit(1)
}
