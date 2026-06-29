package monitor

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"GoNetWatch/internal/models"
	"GoNetWatch/internal/notifier"
	"GoNetWatch/internal/storage"
)

// resolveTimeout returns the per-attempt timeout: explicit config value, or 3s for tcp/dns and 5s for http*.
func resolveTimeout(t models.Target) time.Duration {
	if t.TimeoutSec > 0 {
		return time.Duration(t.TimeoutSec) * time.Second
	}
	switch t.Type {
	case "tcp", "dns":
		return 3 * time.Second
	default:
		return 5 * time.Second
	}
}

// targetKey returns a composite key (name|protocol|address) unique per logical target.
func targetKey(name, protocol, address string) string {
	return name + "|" + protocol + "|" + address
}

// checkDNS resolves t.Address using the configured or system DNS resolver.
// If t.Resolver is set, a custom net.Resolver dials that server directly;
// otherwise the process-default resolver is used.
func checkDNS(t models.Target, timeout time.Duration) models.MonitorResult {
	result := models.MonitorResult{
		Target:   t.Address,
		Protocol: t.Type,
	}

	var r *net.Resolver
	if t.Resolver != "" {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "udp", t.Resolver)
			},
		}
	} else {
		r = net.DefaultResolver
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	addrs, err := r.LookupHost(ctx, t.Address)
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = "FAILURE"
		result.Error = err.Error()
		return result
	}
	if len(addrs) == 0 {
		result.Status = "FAILURE"
		result.Error = "no addresses resolved"
		return result
	}

	result.Success = true
	result.Status = "SUCCESS"
	result.ResolvedCount = len(addrs)
	return result
}

// attemptCheck performs exactly one probe of the target with the given timeout.
// It does not handle retries; that is the sole responsibility of checkTarget.
func attemptCheck(t models.Target, timeout time.Duration) models.MonitorResult {
	result := models.MonitorResult{
		Target:   t.Address,
		Protocol: t.Type,
	}

	switch t.Type {
	case "http", "http-head":
		method := http.MethodGet
		if t.Type == "http-head" {
			method = http.MethodHead
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, method, t.Address, nil)
		if err != nil {
			result.Status = "FAILURE"
			result.Error = err.Error()
			return result
		}
		client := &http.Client{}
		start := time.Now()
		resp, err := client.Do(req)
		result.Latency = time.Since(start)
		if err != nil {
			result.Status = "FAILURE"
			result.Error = err.Error()
			return result
		}
		defer resp.Body.Close()
		result.Code = resp.StatusCode
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			result.Success = true
			result.Status = "SUCCESS"
		} else {
			result.Status = "FAILURE"
		}

	case "tcp":
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var d net.Dialer
		start := time.Now()
		conn, err := d.DialContext(ctx, "tcp", t.Address)
		result.Latency = time.Since(start)
		if err != nil {
			result.Status = "FAILURE"
			result.Error = err.Error()
			return result
		}
		result.Success = true
		result.Status = "SUCCESS"
		if conn != nil {
			conn.Close()
		}

	case "dns":
		return checkDNS(t, timeout)

	default:
		result.Status = "FAILURE"
		result.Error = "unsupported protocol"
	}

	return result
}

// checkTarget probes t, retrying on failure up to t.Retries additional times.
// The returned result reflects the last attempt and carries the Attempts count.
func checkTarget(t models.Target) models.MonitorResult {
	timeout := resolveTimeout(t)

	maxAttempts := 1 + t.Retries
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	retryDelay := time.Duration(t.RetryDelayMs) * time.Millisecond
	if retryDelay <= 0 {
		retryDelay = 300 * time.Millisecond
	}

	var result models.MonitorResult
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result = attemptCheck(t, timeout)
		result.Attempts = attempt
		if result.Success {
			break
		}
		if attempt < maxAttempts {
			time.Sleep(retryDelay)
		}
	}
	// Stamp identity so downstream consumers (logging, InfluxDB, state maps) don't need a reverse lookup.
	result.Name = t.Name
	result.Resolver = t.Resolver
	return result
}

// logResult logs a check result: Info on success, Error on failure.
func logResult(result models.MonitorResult) {
	if result.Success {
		args := []any{
			slog.String("target", result.Name),
			slog.String("address", result.Target),
			slog.String("protocol", result.Protocol),
			slog.Int("code", result.Code),
			slog.Duration("latency", result.Latency),
			slog.Int("attempts", result.Attempts),
		}
		if result.Protocol == "dns" {
			args = append(args, slog.Int("resolved_count", result.ResolvedCount))
		}
		slog.Info("Target check successful", args...)
		return
	}

	slog.Error("Target check failed",
		slog.String("target", result.Name),
		slog.String("address", result.Target),
		slog.String("protocol", result.Protocol),
		slog.Int("code", result.Code),
		slog.String("error", result.Error),
		slog.Int("attempts", result.Attempts),
	)
}

// handleResult logs the result, fires UP/DOWN transition alerts, and writes the metric to InfluxDB.
func handleResult(result models.MonitorResult, targetMap map[string]models.Target, targetStates map[string]bool, notif notifier.Notifier) bool {
	logResult(result)

	// Stable key: unique per logical target even when the same address is
	// probed via different protocols (e.g. TCP and DNS both for github.com).
	key := targetKey(result.Name, result.Protocol, result.Target)
	target, known := targetMap[key]

	// Detect state transitions and notify only on changes to avoid alert spam.
	prevState, exists := targetStates[key]
	if !exists {
		prevState = true // default to UP if not tracked
	}

	if prevState && !result.Success {
		if known && notif != nil {
			if err := notif.OnStateChange(target, result, false); err != nil {
				slog.Error("Failed to send notification",
					slog.String("target", result.Name),
					slog.String("error", err.Error()))
			}
		}
		targetStates[key] = false
	} else if !prevState && result.Success {
		if known && notif != nil {
			if err := notif.OnStateChange(target, result, true); err != nil {
				slog.Error("Failed to send notification",
					slog.String("target", result.Name),
					slog.String("error", err.Error()))
			}
		}
		targetStates[key] = true
	}

	// Write metric asynchronously to InfluxDB (no-op if not configured)
	storage.WriteMetric(result)

	return result.Success
}

// MonitorTargets runs periodic checks for all targets until ctx is canceled, one goroutine per target.
func MonitorTargets(ctx context.Context, targets []models.Target, notif notifier.Notifier) {
	var wg sync.WaitGroup
	// buffer to reduce blocking; allow some backlog
	resultsChan := make(chan models.MonitorResult, len(targets)*4)

	slog.Info("Starting network monitoring", slog.Int("targets", len(targets)))

	startTime := time.Now()

	// Index targets by composite key; prevents collisions when the same address appears in multiple protocols.
	targetMap := make(map[string]models.Target)
	for _, t := range targets {
		targetMap[targetKey(t.Name, t.Type, t.Address)] = t
	}

	// Initialize target states: all targets start as UP (true)
	targetStates := make(map[string]bool)
	for _, t := range targets {
		targetStates[targetKey(t.Name, t.Type, t.Address)] = true
	}

	// Printer goroutine: read results continuously until context is cancelled
	var printerWg sync.WaitGroup
	printerWg.Add(1)
	go func() {
		defer printerWg.Done()
		successCount := 0
		failureCount := 0

		count := func(success bool) {
			if success {
				successCount++
			} else {
				failureCount++
			}
		}

		summary := func() {
			slog.Info("Monitoring completed",
				slog.Duration("duration", time.Since(startTime)),
				slog.Int("successful", successCount),
				slog.Int("failed", failureCount))
		}

		for {
			select {
			case <-ctx.Done():
				// Context cancelled: drain remaining results then exit
				for result := range resultsChan {
					count(handleResult(result, targetMap, targetStates, notif))
				}
				summary()
				return

			case result, ok := <-resultsChan:
				if !ok {
					summary()
					return
				}
				count(handleResult(result, targetMap, targetStates, notif))
			}
		}
	}()

	// Launch a goroutine for each target that runs periodically until ctx is done
	for _, target := range targets {
		wg.Add(1)
		go func(t models.Target) {
			defer wg.Done()

			interval := time.Duration(t.IntervalSec) * time.Second
			if interval <= 0 {
				interval = 60 * time.Second
			}

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			// Do an initial immediate check
			select {
			case <-ctx.Done():
				return
			default:
				res := checkTarget(t)
				resultsChan <- res
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					res := checkTarget(t)
					resultsChan <- res
				}
			}
		}(target)
	}

	// Wait for all target goroutines to exit, then close results channel so
	// the printer goroutine can finish.
	wg.Wait()
	close(resultsChan)
	printerWg.Wait()
}
