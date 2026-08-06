package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/greenroute/greenroute/internal/telemetry"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck(os.Args[2:]))
	}

	logger, shutdownLogging, logErr := telemetry.SetupLogging(context.Background(), "provider-yandex", slog.LevelInfo)
	if logErr != nil {
		fmt.Fprintln(os.Stderr, "logging setup failed:", logErr)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownLogging(ctx)
	}()
	cfg, err := LoadConfig()
	if err != nil {
		logger.Error("configuration rejected", "error", err.Error())
		os.Exit(2)
	}
	if cfg.ExperimentalSourcesRequested {
		logger.Warn("experimental provider sources were requested but are intentionally unsupported; official or stub adapter remains active")
	}

	metrics := &serviceMetrics{}
	var provider Provider
	switch cfg.ProviderMode {
	case providerModeStub:
		provider = newStubAdapter(cfg, metrics)
	case providerModeYandex:
		provider = newYandexAdapter(cfg, nil, metrics)
	case providerModeDGIS:
		provider = newDGISAdapter(cfg, nil, metrics)
	default:
		logger.Error("unsupported provider mode")
		os.Exit(2)
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newAPIServer(cfg, provider, metrics, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown failed", "error", err.Error())
		}
	}()

	logger.Info("provider service starting", "addr", cfg.HTTPAddr, "mode", cfg.ProviderMode)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("provider service stopped unexpectedly", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("provider service stopped")
}

func runHealthcheck(args []string) int {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	endpoint := flags.String("url", "http://127.0.0.1:8082/health/ready", "readiness URL")
	timeout := flags.Duration("timeout", 2*time.Second, "request timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	client := &http.Client{Timeout: *timeout}
	request, err := http.NewRequest(http.MethodGet, *endpoint, nil)
	if err != nil {
		return 2
	}
	response, err := client.Do(request)
	if err != nil {
		return 1
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
