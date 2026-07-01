package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	cfgpkg "GoNetWatch/internal/config"
	"GoNetWatch/internal/importer"
	"GoNetWatch/internal/monitor"
	"GoNetWatch/internal/notifier"
	"GoNetWatch/internal/storage"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if len(os.Args) > 1 && os.Args[1] == "import-targets" {
		fs := flag.NewFlagSet("import-targets", flag.ExitOnError)
		input := fs.String("input", "", "input file path (required)")
		output := fs.String("output", "configs/config.yaml", "output config file path")
		defaultType := fs.String("default-type", "http-head", "default target type: http, http-head, tcp, dns")
		interval := fs.Int("interval", 30, "default interval_sec for imported targets")
		timeout := fs.Int("timeout", 0, "default timeout_sec for imported targets")
		retries := fs.Int("retries", 0, "default retries for imported targets")
		retryDelayMS := fs.Int("retry-delay-ms", 300, "default retry_delay_ms for imported targets")
		appendMode := fs.Bool("append", true, "append to existing config instead of replacing")
		dryRun := fs.Bool("dry-run", false, "print what would be done without writing files")
		_ = fs.Parse(os.Args[2:])

		opts := importer.ImportOptions{
			Input:        *input,
			Output:       *output,
			DefaultType:  *defaultType,
			Interval:     *interval,
			Timeout:      *timeout,
			Retries:      *retries,
			RetryDelayMS: *retryDelayMS,
			Append:       *appendMode,
			DryRun:       *dryRun,
		}
		if err := importer.Run(opts); err != nil {
			slog.Error("import-targets failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		return
	}

	configPath := "configs/config.yaml"
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		configPath = p
	}
	cfg, err := cfgpkg.LoadConfig(configPath)
	if err != nil {
		slog.Error("Failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := cfgpkg.Validate(cfg); err != nil {
		slog.Error("Invalid configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := storage.Init(cfg.InfluxDB); err != nil {
		slog.Error("Failed to initialize storage", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer storage.Close()

	var notif notifier.Notifier
	if strings.TrimSpace(cfg.Telegram.BotToken) == "" {
		slog.Info("Telegram notifications disabled")
	} else if len(cfg.Telegram.ChatIDs) == 0 {
		slog.Info("Telegram notifications disabled")
	} else {
		notif = notifier.NewTelegramNotifier(cfg.Telegram)
	}

	targets := cfg.Targets

	if len(targets) == 0 {
		slog.Error("No targets found in configs/config.yaml (expected 'targets' array)")
		os.Exit(1)
	}

	slog.Info("GoNetWatch starting", slog.Int("targets", len(targets)))

	if notif != nil {
		if err := notif.OnStart(len(targets)); err != nil {
			slog.Error("Failed to send start notification", slog.String("error", err.Error()))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		monitor.MonitorTargets(ctx, targets, notif)
		close(done)
	}()

	// Listen for termination signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	sig := <-sigCh
	slog.Info("Received shutdown signal", slog.String("signal", sig.String()))

	if notif != nil {
		if err := notif.OnStop(); err != nil {
			slog.Error("Failed to send stop notification", slog.String("error", err.Error()))
		}
	}

	cancel()

	// Wait for monitor to finish

	select {
	case <-done:
		slog.Info("Shutdown complete")
	case <-time.After(15 * time.Second):
		slog.Warn("Timeout waiting for shutdown; exiting")
	}
}
