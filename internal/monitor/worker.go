package monitor

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"GoNetWatch/internal/models"
)

// checkTarget performs an HTTP GET request for HTTP targets. Non-HTTP
// protocols are currently reported as unsupported.
func checkTarget(t models.Target) models.MonitorResult {
	result := models.MonitorResult{
		Target: t.Address,
	}

	if t.Protocol != "http" && t.Protocol != "https" {
		result.Success = false
		result.Status = "FAILURE"
		result.Error = "unsupported protocol"
		return result
	}

	client := &http.Client{Timeout: 10 * time.Second}

	start := time.Now()
	resp, err := client.Get(t.Address)
	latency := time.Since(start)
	result.Latency = latency

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

	return result
}

func printResult(result models.MonitorResult) {
	if result.Success {
		fmt.Printf("[%s] Target: %s | Status: %d | Latency: %dms\n",
			result.Status, result.Target, result.Code, result.Latency.Milliseconds())
	} else {
		if result.Code == 0 {
			fmt.Printf("[%s] Target: %s | Error: %s\n",
				result.Status, result.Target, result.Error)
		} else {
			fmt.Printf("[%s] Target: %s | Status: %d | Latency: %dms\n",
				result.Status, result.Target, result.Code, result.Latency.Milliseconds())
		}
	}
}

// MonitorTargets concurrently monitors all provided targets.
func MonitorTargets(targets []models.Target) {
	var wg sync.WaitGroup
	resultsChan := make(chan models.MonitorResult, len(targets))

	fmt.Println("========== Network Monitoring System ==========")
	fmt.Printf("Starting monitoring of %d target(s)...\n", len(targets))
	fmt.Println("==============================================")
	fmt.Println()

	startTime := time.Now()

	for _, target := range targets {
		wg.Add(1)
		go func(t models.Target) {
			defer wg.Done()
			res := checkTarget(t)
			resultsChan <- res
		}(target)
	}

	wg.Wait()
	close(resultsChan)

	successCount := 0
	failureCount := 0

	for result := range resultsChan {
		printResult(result)
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
}
