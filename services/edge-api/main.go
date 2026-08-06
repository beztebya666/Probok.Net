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

	"github.com/greenroute/greenroute/internal/telemetry"
	"github.com/redis/go-redis/v9"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			if err := runHealthcheck(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "migrate":
			if len(os.Args) != 3 || os.Args[2] != "up" {
				fmt.Fprintln(os.Stderr, "usage: edge-api migrate up")
				os.Exit(2)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := runMigrations(ctx, os.Getenv("DATABASE_URL")); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
	if err := run(); err != nil {
		slog.Error("edge-api stopped", "error", err)
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
	logger, shutdownLogging, err := telemetry.SetupLogging(root, "edge-api", level)
	if err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}
	slog.SetDefault(logger)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownLogging(ctx)
	}()
	shutdownTracing, err := telemetry.SetupTracing(root, "edge-api")
	if err != nil {
		return fmt.Errorf("setup tracing: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(ctx)
	}()
	metrics := telemetry.NewMetrics("edge-api")
	state, err := configureState(root, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = state.Close() }()
	auditor, err := newAuditor(root, cfg.DatabaseURL, cfg.AuditHashKey, cfg.RequireDatabase)
	if err != nil {
		return fmt.Errorf("configure audit database: %w", err)
	}
	defer auditor.close()
	auditor.runRetention(root, cfg.AuditRetention, cfg.AuditPurgeInterval,
		func(rows int64) { metrics.AuditRowsPurged.Add(float64(rows)) },
		func(err error) {
			metrics.AuditFailures.WithLabelValues("retention").Inc()
			slog.Error("audit retention purge failed", "error_type", fmt.Sprintf("%T", err))
		},
	)
	authContext, cancelAuth := context.WithTimeout(root, 5*time.Second)
	auth, err := newAuthenticator(authContext, cfg)
	cancelAuth()
	if err != nil {
		return fmt.Errorf("configure authentication: %w", err)
	}
	services, err := newServiceClient(cfg.OrchestratorURL, cfg.ProviderURL, cfg.InternalToken)
	if err != nil {
		return err
	}
	handler, err := newAPIServer(cfg, auth, state, auditor, services, metrics)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: cfg.Address, Handler: handler, ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout: 20 * time.Second, IdleTimeout: 65 * time.Second, MaxHeaderBytes: 32 << 10,
	}
	errorsChannel := make(chan error, 1)
	go func() {
		slog.Info("edge-api listening", "address", cfg.Address)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	select {
	case err := <-errorsChannel:
		return err
	case <-root.Done():
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancel()
	return server.Shutdown(shutdownContext)
}

func configureState(ctx context.Context, cfg config) (edgeState, error) {
	if cfg.RedisURL == "" {
		if cfg.RequireRedis {
			return nil, fmt.Errorf("redis is required")
		}
		slog.Info("using in-process rate, idempotency, and ownership state")
		return newMemoryState(), nil
	}
	options, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	state := newRedisState(options)
	pingContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := state.Ping(pingContext); err != nil {
		_ = state.Close()
		if cfg.RequireRedis {
			return nil, fmt.Errorf("redis required but unavailable: %w", err)
		}
		slog.Warn("Redis unavailable; using in-process state", "error", err)
		return newMemoryState(), nil
	}
	return state, nil
}

func runHealthcheck(arguments []string) error {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := flags.String("url", "http://127.0.0.1:8080/health/ready", "readiness URL")
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
