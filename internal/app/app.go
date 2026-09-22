package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ppxb/miyabi"
	"github.com/ppxb/miyabi/internal/api"
	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/config"
	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/drive"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/javdb"
	"github.com/ppxb/miyabi/internal/library"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/maintenance"
	"github.com/ppxb/miyabi/internal/monitor"
	"github.com/ppxb/miyabi/internal/offline"
	"github.com/ppxb/miyabi/internal/playback"
	"github.com/ppxb/miyabi/internal/tasks"
)

// App is the composition root assembling services, background workers, and the HTTP server.
type App struct {
	cfg       *config.Config
	logger    *slog.Logger
	store     *database.Store
	server    *http.Server
	pool      *tasks.Pool
	offline   *offline.Service
	monitors  *monitor.Service
	driveSvc  *drive.Drive
	catalogue *catalogue.Service
	play      *playback.Service
}

// New initializes all services, database connections, and registers task handlers.
func New(cfg *config.Config, logger *slog.Logger) (*App, error) {
	ctx := context.Background()
	store, err := database.Open(ctx, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := library.MigrateViewedMovies(ctx, store.Client); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("migrate viewed movies: %w", err)
	}

	network, err := NewNetworkService(ctx, store.Client)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("initialize network service: %w", err)
	}

	taskRegistry := tasks.NewRegistry()
	taskSvc := tasks.NewService(store.Client, taskRegistry)
	driveSvc, err := drive.New(ctx, store.Client)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("initialize drive service: %w", err)
	}

	images, err := mediaimage.NewCache(cfg.DataDir)
	if err != nil {
		driveSvc.Close()
		_ = store.Close()
		return nil, fmt.Errorf("initialize images cache: %w", err)
	}

	libSvc := library.New(store.Client, driveSvc, taskSvc, images)
	catalogueSvc, err := catalogue.New(ctx, store.Client, javdb.Options{}, network.ProxyManager(), libSvc)
	if err != nil {
		driveSvc.Close()
		_ = store.Close()
		return nil, fmt.Errorf("initialize catalogue service: %w", err)
	}

	offlineSvc := offline.New(store.Client, catalogueSvc, driveSvc, taskSvc, libSvc)
	monitorSvc := monitor.New(store.Client, catalogueSvc, offlineSvc, taskSvc)
	playSvc := playback.New(store.Client, driveSvc)
	scrapeSvc := scrape.New(store.Client, driveSvc, catalogueSvc, images, taskSvc)
	maintenanceSvc, err := maintenance.New(cfg.DataDir, store.Client, images, scrapeSvc)
	if err != nil {
		playSvc.Close()
		catalogueSvc.Close()
		driveSvc.Close()
		_ = store.Close()
		return nil, fmt.Errorf("initialize maintenance service: %w", err)
	}

	taskRegistry.Register(tasks.NewHandler(tasks.KindScan, libSvc.Scan, libSvc.Finished))
	taskRegistry.Register(tasks.NewHandler(tasks.KindScrape, scrapeSvc.Scrape, scrapeSvc.Finished))
	taskRegistry.Register(tasks.NewHandler(tasks.KindCover, scrapeSvc.Cover, scrapeSvc.Finished))

	pool := tasks.NewPool(taskSvc.Queue(), taskSvc.Bus(), taskRegistry, 1, logger)

	router := api.NewRouter(api.Dependencies{
		Logger:      logger,
		Health:      store,
		Access:      api.NewAccessGateService(cfg.AccessPassword),
		Catalogue:   catalogueSvc,
		Drive:       driveSvc,
		Offline:     offlineSvc,
		Monitor:     monitorSvc,
		Library:     libSvc,
		Play:        playSvc,
		Tasks:       taskSvc,
		Artwork:     scrapeSvc,
		Maintenance: maintenanceSvc,
		Network:     network,
		Frontend:    miyabi.Frontend(),
	})

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return &App{
		cfg:       cfg,
		logger:    logger,
		store:     store,
		server:    server,
		pool:      pool,
		offline:   offlineSvc,
		monitors:  monitorSvc,
		driveSvc:  driveSvc,
		catalogue: catalogueSvc,
		play:      playSvc,
	}, nil
}

// Run starts the task pool, the periodic workers and the HTTP server, then
// blocks until ctx is cancelled or one of them fails. Every exit path shuts
// the server down gracefully and waits for the workers before returning.
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.server.BaseContext = func(net.Listener) context.Context { return ctx }

	var workers sync.WaitGroup
	poolError := make(chan error, 1)
	workers.Add(3)
	go func() {
		defer workers.Done()
		poolError <- a.pool.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		tasks.RunPeriodic(ctx, a.logger, "sync 115 offline tasks", 30*time.Second, nil, a.offline.Sync)
	}()
	go func() {
		defer workers.Done()
		tasks.RunPeriodic(ctx, a.logger, "monitor", 5*time.Minute, a.monitors.Pending(), a.monitors.Check)
	}()

	serverError := make(chan error, 1)
	go func() {
		a.logger.Info("HTTP server started", "address", a.cfg.Listen)
		serverError <- a.server.ListenAndServe()
	}()

	var runError error
	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			runError = fmt.Errorf("serve HTTP: %w", err)
		}
	case err := <-poolError:
		if err != nil {
			runError = fmt.Errorf("run task pool: %w", err)
		}
	case <-ctx.Done():
	}

	cancel()
	shutdownContext, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := a.server.Shutdown(shutdownContext); err != nil {
		runError = errors.Join(runError, fmt.Errorf("shutdown HTTP server: %w", err))
	}
	workers.Wait()
	a.logger.Info("HTTP server stopped")
	return runError
}

// Close releases resources held by the application.
func (a *App) Close() error {
	if a.play != nil {
		a.play.Close()
	}
	if a.catalogue != nil {
		a.catalogue.Close()
	}
	if a.driveSvc != nil {
		a.driveSvc.Close()
	}
	if a.store != nil {
		return a.store.Close()
	}
	return nil
}

// CheckHealth probes the health of a running server given its listen address.
func CheckHealth(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("parse health check address: %w", err)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	client := &http.Client{
		Timeout: 4 * time.Second,
		// A local probe must not use proxy environment variables.
		Transport: &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/api/health")
	if err != nil {
		return fmt.Errorf("check health: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned HTTP %d", response.StatusCode)
	}
	return nil
}
