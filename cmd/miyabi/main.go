package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ppxb/miyabi"
	"github.com/ppxb/miyabi/internal/api"
	"github.com/ppxb/miyabi/internal/config"
	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/drive"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/javdb"
	"github.com/ppxb/miyabi/internal/library"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/logging"
	"github.com/ppxb/miyabi/internal/service"
	"github.com/ppxb/miyabi/internal/tasks"
	"github.com/ppxb/miyabi/internal/worker"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("miyabi stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	healthcheck := len(args) == 1 && args[0] == "healthcheck"
	if len(args) > 0 && !healthcheck {
		return errors.New("usage: miyabi [healthcheck]; configure the application with MIYABI_* environment variables")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if healthcheck {
		return checkHealth(cfg.Listen)
	}

	logger, err := logging.New(os.Stdout, cfg.LogLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	store, err := database.Open(context.Background(), cfg.DataDir)
	if err != nil {
		return err
	}
	defer store.Close()
	network, err := service.NewNetworkService(context.Background(), store.Client)
	if err != nil {
		return fmt.Errorf("initialize network service: %w", err)
	}
	taskRegistry := tasks.NewRegistry()
	taskSvc := tasks.NewService(store.Client, taskRegistry)
	driveSvc, err := drive.New(context.Background(), store.Client)
	if err != nil {
		return fmt.Errorf("initialize drive service: %w", err)
	}
	defer driveSvc.Close()
	discover, err := service.NewDiscoverService(context.Background(), store.Client, javdb.Options{}, network.ProxyManager(), driveSvc)
	if err != nil {
		return fmt.Errorf("initialize discovery service: %w", err)
	}
	defer discover.Close()
	offline := service.NewOfflineService(store.Client, discover, driveSvc, taskSvc)
	monitors := service.NewMonitorService(store.Client, discover, offline, taskSvc)
	images, err := mediaimage.NewCache(cfg.DataDir)
	if err != nil {
		return err
	}
	library := library.New(store.Client, driveSvc, taskSvc, images)
	play := service.NewPlayService(library, driveSvc)
	defer play.Close()
	scrapeSvc := scrape.New(store.Client, driveSvc, discover, images, taskSvc)
	data, err := service.NewDataService(cfg.DataDir, store.Client, images, scrapeSvc)
	if err != nil {
		return fmt.Errorf("initialize data service: %w", err)
	}
	taskRegistry.Register(tasks.NewHandler(tasks.KindScan, library.Scan, library.Finished))
	taskRegistry.Register(tasks.NewHandler(tasks.KindScrape, scrapeSvc.Scrape, scrapeSvc.Finished))
	taskRegistry.Register(tasks.NewHandler(tasks.KindCover, scrapeSvc.Cover, scrapeSvc.Finished))
	// Keep scans, metadata writes and directory sidecars ordered.
	pool := tasks.NewPool(taskSvc.Queue(), taskSvc.Bus(), taskRegistry, 1, logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	router := api.NewRouter(api.Dependencies{
		Logger:   logger,
		Health:   store,
		Access:   service.NewAccessGateService(cfg.AccessPassword),
		Discover: discover,
		Pan:      driveSvc,
		Offline:  offline,
		Monitor:  monitors,
		Library:  library,
		Play:     play,
		Tasks:    taskSvc,
		Artwork:  scrapeSvc,
		Data:     data,
		Network:  network,
		Frontend: miyabi.Frontend(),
	})
	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	workerDone := make(chan struct{})
	monitorDone := make(chan struct{})
	poolDone := make(chan struct{})
	poolError := make(chan error, 1)
	go func() {
		defer close(poolDone)
		poolError <- pool.Run(ctx)
	}()
	go func() {
		defer close(workerDone)
		worker.RunOffline(ctx, offline, logger)
	}()
	go func() {
		defer close(monitorDone)
		worker.RunMonitor(ctx, monitors, logger)
	}()
	defer func() {
		stop()
		<-workerDone
		<-monitorDone
		<-poolDone
	}()

	serverError := make(chan error, 1)
	go func() {
		logger.Info("HTTP server started", "address", cfg.Listen)
		serverError <- server.ListenAndServe()
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
	stop()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return errors.Join(runError, fmt.Errorf("shutdown HTTP server: %w", err))
	}

	logger.Info("HTTP server stopped")
	return runError
}
