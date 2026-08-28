// Command pimonitor serves the PiMonitor system-monitoring dashboard and
// its REST API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/larslaskowski/pimonitor/internal/alert"
	"github.com/larslaskowski/pimonitor/internal/collector"
	"github.com/larslaskowski/pimonitor/internal/config"
	"github.com/larslaskowski/pimonitor/internal/httpapi"
	"github.com/larslaskowski/pimonitor/internal/web"
)

// version is set via -ldflags "-X main.version=..." in release builds.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pimonitor:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	result, err := config.Load(args)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if result.VersionRequested {
		fmt.Println("pimonitor " + version)
		return nil
	}
	cfg := result.Config

	log := newLogger(cfg.LogLevel)

	notifier, err := alert.NewNotifier(cfg.Alerts, log)
	if err != nil {
		return fmt.Errorf("configure alert webhooks: %w", err)
	}
	warnIfNotifierInert(log, cfg.Alerts, notifier)

	collCfg := collector.Config{
		FastInterval:          cfg.FastInterval(),
		SlowInterval:          cfg.SlowInterval(),
		HistoryCapacity:       cfg.HistoryCapacity(),
		NetworkEnabled:        cfg.NetworkEnabled,
		UpdatesStaleThreshold: cfg.UpdatesStaleThreshold(),
		DistroInfoEnabled:     cfg.DistroInfoEnabled,
		PiModelEnabled:        cfg.PiModelEnabled,
		HistoryWindow:         cfg.HistoryWindow(),
		AlertsEnabled:         cfg.Alerts.Enabled,
		AlertFor:              cfg.AlertFor(),
		Thresholds:            cfg.Thresholds,
		Notifier:              notifier,
	}
	if cfg.HistoryPersistEnabled {
		collCfg.PersistPath = filepath.Join(cfg.DataDir, "history.bin")
	}
	coll := collector.New(collCfg, log)

	staticHandler, err := web.Handler()
	if err != nil {
		return fmt.Errorf("load embedded web assets: %w", err)
	}

	server := httpapi.New(coll, httpapi.Config{
		ListenAddr:          cfg.ListenAddr,
		APIKey:              cfg.APIKey,
		HealthzMaxStaleness: cfg.HealthzMaxStaleness(),
		Client:              clientConfig(cfg, version),
	}, staticHandler, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	collDone := make(chan struct{})
	go func() {
		coll.Run(ctx)
		close(collDone)
	}()

	serveErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		// Wait for the collector to finish its final history flush before
		// the process exits.
		select {
		case <-collDone:
		case <-shutdownCtx.Done():
		}
		return nil
	case err := <-serveErr:
		return err
	}
}

// clientConfig maps the resolved configuration to the non-sensitive shape
// served by GET /api/v1/config. Thresholds is passed through as-is (see
// httpapi.ClientConfig), since config.Thresholds already carries the json
// tags the API response needs.
func clientConfig(cfg config.Config, version string) httpapi.ClientConfig {
	return httpapi.ClientConfig{
		Version:              version,
		PollIntervalSeconds:  cfg.PollIntervalSeconds,
		HistoryWindowMinutes: cfg.HistoryWindowMinutes,
		NetworkEnabled:       cfg.NetworkEnabled,
		Thresholds:           cfg.Thresholds,
	}
}

// warnIfNotifierInert logs a startup warning when webhooks are configured
// (notifier is non-nil) but alerts.enabled is false: a disabled alert engine
// never produces the transition events those webhooks would need, so the
// combination silently never fires without this hint.
func warnIfNotifierInert(log *slog.Logger, alerts config.Alerts, notifier *alert.Notifier) {
	if notifier != nil && !alerts.Enabled {
		log.Warn("alert webhooks configured but alerts.enabled is false — no notifications will be sent")
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
