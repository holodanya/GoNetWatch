package monitor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"GoNetWatch/internal/models"
	"GoNetWatch/internal/notifier"
	"GoNetWatch/internal/storage"
)

// checkTarget performs an HTTP GET request for HTTP targets. Non-HTTP
// protocols are currently reported as unsupported.
func checkTarget(t models.Target) models.MonitorResult {
	result := models.MonitorResult{
		Target:   t.Address,
		Protocol: t.Type,
	}

	// Use a consistent timeout for all checks
	timeout := 10 * time.Second

	switch t.Type {
	case "http":
		client := &http.Client{Timeout: timeout}
		start := time.Now()
		resp, err := client.Get(t.Address)
		result.Latency = time.Since(start)
		if err != nil {
			result.Success = false
			result.Status = "FAILURE"
			result.Error = err.Error()
			result.Code = 0
			return result
		}
		defer resp.Body.Close()
		result.Code = resp.StatusCode
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			result.Success = true
			result.Status = "SUCCESS"
		} else {
			result.Success = false
			result.Status = "FAILURE"
		}

	case "http-head":
		// HEAD is more efficient than GET for uptime checks because it
		// retrieves only the response headers and not the full body, which
		// reduces bandwidth and latency when you only need status information.
		client := &http.Client{Timeout: timeout}
		start := time.Now()
		resp, err := client.Head(t.Address)
		result.Latency = time.Since(start)
		if err != nil {
			result.Success = false
			result.Status = "FAILURE"
			result.Error = err.Error()
			result.Code = 0
			return result
		}
		defer resp.Body.Close()
		result.Code = resp.StatusCode
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			result.Success = true
			result.Status = "SUCCESS"
		} else {
			result.Success = false
			result.Status = "FAILURE"
		}

	case "tcp":
		// For TCP we measure the time to establish a connection to the
		// remote host:port. If DialTimeout succeeds the port is open.
		start := time.Now()
		conn, err := net.DialTimeout("tcp", t.Address, timeout)
		result.Latency = time.Since(start)
		if err != nil {
			result.Success = false
			result.Status = "FAILURE"
			result.Error = err.Error()
			result.Code = 0
			return result
		}
		result.Success = true
		result.Status = "SUCCESS"
		result.Code = 0
		if conn != nil {
			conn.Close()
		}

	default:
		result.Success = false
		result.Status = "FAILURE"
		result.Error = "unsupported protocol"
		result.Code = 0
		return result
	}

	return result
}

func printResult(result models.MonitorResult) {
	// ANSI color codes
	const (
		colorGreen = "\033[32m"
		colorRed   = "\033[31m"
		colorReset = "\033[0m"
	)

	// Timestamp in HH:MM:SS
	ts := time.Now().Format("15:04:05")

	// Format status tag with color
	var statusTag string
	if result.Success {
		statusTag = fmt.Sprintf("%s[SUCCESS]%s", colorGreen, colorReset)
	} else {
		statusTag = fmt.Sprintf("%s[FAILURE]%s", colorRed, colorReset)
	}

	protoTag := ""
	if result.Protocol != "" {
		protoTag = fmt.Sprintf("[%s]", strings.ToUpper(result.Protocol))
	}

	if result.Success {
		if result.Code > 0 {
			fmt.Printf("[%s] %s %s Target: %s | Status: %d | Latency: %dms\n",
				ts, statusTag, protoTag, result.Target, result.Code, result.Latency.Milliseconds())
		} else {
			// e.g., TCP success (no HTTP status code)
			fmt.Printf("[%s] %s %s Target: %s | Latency: %dms\n",
				ts, statusTag, protoTag, result.Target, result.Latency.Milliseconds())
		}
	} else {
		if result.Code == 0 {
			fmt.Printf("[%s] %s %s Target: %s | Error: %s%s%s\n",
				ts, statusTag, protoTag, result.Target, colorRed, result.Error, colorReset)
		} else {
			fmt.Printf("[%s] %s %s Target: %s | Status: %d | Latency: %dms\n",
				ts, statusTag, protoTag, result.Target, result.Code, result.Latency.Milliseconds())
		}
	}
}

// MonitorTargets runs checks for each target periodically until the provided
// context is canceled. Each target runs in its own goroutine; results are sent
// to a printer goroutine which runs until resultsChan is closed.
// Uses a state machine to detect UP/DOWN transitions and prevent alert spam.
func MonitorTargets(ctx context.Context, targets []models.Target, notif notifier.Notifier) {
	var wg sync.WaitGroup
	// buffer to reduce blocking; allow some backlog
	resultsChan := make(chan models.MonitorResult, len(targets)*4)

	fmt.Println("========== Network Monitoring System ==========")
	fmt.Printf("Starting monitoring of %d target(s)...\n", len(targets))
	fmt.Println("==============================================")
	fmt.Println()

	startTime := time.Now()

	// Build a map of targets indexed by their address for easy lookup
	targetMap := make(map[string]models.Target)
	for _, t := range targets {
		targetMap[t.Address] = t
	}

	// Initialize target states: all targets start as UP (true)
	targetStates := make(map[string]bool)
	for _, t := range targets {
		targetStates[t.Address] = true
	}

	// Printer goroutine: read results continuously until context is cancelled
	var printerWg sync.WaitGroup
	printerWg.Add(1)
	go func() {
		defer printerWg.Done()
		successCount := 0
		failureCount := 0

		for {
			select {
			case <-ctx.Done():
				// Context cancelled: drain remaining results then exit
				for result := range resultsChan {
					printResult(result)
					// Check for state transitions and notify
					prevState, exists := targetStates[result.Target]
					if !exists {
						prevState = true // default to UP if not tracked
					}
					if prevState && !result.Success {
						// Transition to DOWN
						if target, ok := targetMap[result.Target]; ok && notif != nil {
							if err := notif.OnStateChange(target, result, false); err != nil {
								fmt.Printf("Error sending notification: %v\n", err)
							}
						}
						targetStates[result.Target] = false
					} else if !prevState && result.Success {
						// Transition to UP
						if target, ok := targetMap[result.Target]; ok && notif != nil {
							if err := notif.OnStateChange(target, result, true); err != nil {
								fmt.Printf("Error sending notification: %v\n", err)
							}
						}
						targetStates[result.Target] = true
					}
					// persist metric for drained results as well (no-op if storage not initialized)
					storage.WriteMetric(result)
					if result.Success {
						successCount++
					} else {
						failureCount++
					}
				}

				totalTime := time.Since(startTime)
				fmt.Println()
				fmt.Println("==============================================")
				fmt.Printf("Monitoring completed in %dms\n", totalTime.Milliseconds())
				fmt.Printf("Summary: %d successful | %d failed\n", successCount, failureCount)
				fmt.Println("==============================================")
				return

			case result, ok := <-resultsChan:
				if !ok {
					// Channel closed: print summary and exit
					totalTime := time.Since(startTime)
					fmt.Println()
					fmt.Println("==============================================")
					fmt.Printf("Monitoring completed in %dms\n", totalTime.Milliseconds())
					fmt.Printf("Summary: %d successful | %d failed\n", successCount, failureCount)
					fmt.Println("==============================================")
					return
				}
				printResult(result)

				// Check for state transitions and notify only on changes
				prevState, exists := targetStates[result.Target]
				if !exists {
					prevState = true // default to UP if not tracked
				}

				if prevState && !result.Success {
					// Transition to DOWN
					if target, ok := targetMap[result.Target]; ok && notif != nil {
						if err := notif.OnStateChange(target, result, false); err != nil {
							fmt.Printf("Error sending notification: %v\n", err)
						}
					}
					targetStates[result.Target] = false
				} else if !prevState && result.Success {
					// Transition to UP
					if target, ok := targetMap[result.Target]; ok && notif != nil {
						if err := notif.OnStateChange(target, result, true); err != nil {
							fmt.Printf("Error sending notification: %v\n", err)
						}
					}
					targetStates[result.Target] = true
				}

				// Write metric asynchronously to InfluxDB (no-op if not configured)
				storage.WriteMetric(result)
				if result.Success {
					successCount++
				} else {
					failureCount++
				}
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
	// Wait for the printer to finish printing the remaining results
	printerWg.Wait()
}
