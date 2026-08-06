package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/greenroute/greenroute/internal/scoring"
	"github.com/greenroute/greenroute/internal/searchstore"
	"github.com/greenroute/greenroute/internal/telemetry"
	"github.com/redis/go-redis/v9"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("routing-orchestrator stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	root, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger, shutdownLogging, err := telemetry.SetupLogging(root, "routing-orchestrator", level)
	if err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}
	slog.SetDefault(logger)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownLogging(ctx)
	}()
	shutdownTracing, err := telemetry.SetupTracing(root, "routing-orchestrator")
	if err != nil {
		return fmt.Errorf("setup tracing: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(ctx)
	}()
	metrics := telemetry.NewMetrics("routing-orchestrator")
	store, err := configureStore(root, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	provider, err := newProviderClient(cfg.ProviderURL, cfg.InternalToken, metrics)
	if err != nil {
		return err
	}
	engine := newEngine(root, cfg, store, provider, metrics)
	policy, err := scoring.LoadConfigFile(cfg.ScoringPolicyFile)
	if err != nil {
		return err
	}
	engine.scoring = policy
	go engine.refreshCapabilities(root)
	go engine.recoverStaleSearches(root)

	httpServer := &http.Server{
		Addr: cfg.Address, Handler: newAPIServer(cfg, engine, store),
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 65 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	errorsChannel := make(chan error, 1)
	go func() {
		slog.Info("routing-orchestrator listening", "address", cfg.Address)
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	select {
	case err := <-errorsChannel:
		return err
	case <-root.Done():
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancelShutdown()
	return errors.Join(httpServer.Shutdown(shutdownContext), engine.shutdown(shutdownContext))
}

func configureStore(ctx context.Context, cfg config) (searchstore.Store, error) {
	if !cfg.ProviderDataStorage || cfg.RedisURL == "" {
		slog.Info("using transient in-process route state", "reason", "provider data storage not enabled or Redis not configured")
		return searchstore.NewMemory(), nil
	}
	options, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	store := searchstore.NewRedis(options, "greenroute")
	pingContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := store.Ping(pingContext); err != nil {
		_ = store.Close()
		if cfg.RequireRedis {
			return nil, fmt.Errorf("redis required but unavailable: %w", err)
		}
		slog.Warn("Redis unavailable; using in-process route state", "error", err)
		return searchstore.NewMemory(), nil
	}
	return store, nil
}

func runHealthcheck(arguments []string) error {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := flags.String("url", "http://127.0.0.1:8081/health/ready", "readiness URL")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(*url)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}
