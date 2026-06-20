package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	cfgpkg "GoNetWatch/internal/config"
	"GoNetWatch/internal/monitor"
	"GoNetWatch/internal/notifier"
	"GoNetWatch/internal/storage"
)

func main() {
	cfg, err := cfgpkg.LoadConfig("configs/config.yaml")
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Initialize storage (InfluxDB) if configured — do it immediately after loading config
	if err := storage.Init(cfg.InfluxDB); err != nil {
		fmt.Printf("Error initializing storage: %v\n", err)
		os.Exit(1)
	}
	defer storage.Close()

	// Initialize notifier (Telegram) if configured
	var notif notifier.Notifier
	if cfg.Telegram.BotToken != "" && len(cfg.Telegram.ChatIDs) > 0 {
		notif = notifier.NewTelegramNotifier(cfg.Telegram)
	}

	// Use targets directly from config (each target should specify type, address, interval_sec)
	targets := cfg.Targets

	if len(targets) == 0 {
		fmt.Println("No targets found in configs/config.yaml (expected 'targets' array)")
		os.Exit(1)
	}

	// Send OnStart notification if notifier is configured
	if notif != nil {
		if err := notif.OnStart(len(targets)); err != nil {
			fmt.Printf("Error sending start notification: %v\n", err)
		}
	}

	// Create a cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run monitor in background
	done := make(chan struct{})
	go func() {
		monitor.MonitorTargets(ctx, targets, notif)
		close(done)
	}()

	// Listen for termination signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	sig := <-sigCh
	fmt.Printf("Received signal %v — shutting down...\n", sig)

	// Send OnStop notification if notifier is configured
	if notif != nil {
		if err := notif.OnStop(); err != nil {
			fmt.Printf("Error sending stop notification: %v\n", err)
		}
	}

	// Signal monitor to stop
	cancel()

	// Wait for monitor to finish
	select {
	case <-done:
		fmt.Println("Shutdown complete.")
	case <-time.After(15 * time.Second):
		fmt.Println("Timeout waiting for shutdown; exiting.")
	}
}
