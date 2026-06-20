package storage

import (
	"fmt"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"

	"GoNetWatch/internal/models"
)

var (
	client   influxdb2.Client
	writeAPI api.WriteAPI
	errCh    <-chan error
)

// Init initializes the InfluxDB client and an asynchronous WriteAPI.
// If cfg.URL is empty, initialization is skipped.
func Init(cfg models.InfluxDBConfig) error {
	if cfg.URL == "" || cfg.Token == "" || cfg.Org == "" || cfg.Bucket == "" {
		// No InfluxDB configured; skip initialization
		return nil
	}

	client = influxdb2.NewClient(cfg.URL, cfg.Token)
	writeAPI = client.WriteAPI(cfg.Org, cfg.Bucket)

	// capture errors from the async write API
	errCh = writeAPI.Errors()
	go func() {
		for err := range errCh {
			// Log async write errors; don't crash the process
			fmt.Printf("[INFLUX] write error: %v\n", err)
		}
	}()

	return nil
}

// WriteMetric writes a single MonitorResult to InfluxDB asynchronously.
// It is non-blocking (uses WriteAPI). If Init wasn't called, this is a no-op.
func WriteMetric(result models.MonitorResult) {
	if writeAPI == nil {
		return
	}

	// Prepare point
	measurement := "network_latency"
	tags := map[string]string{
		"target":   result.Target,
		"protocol": result.Protocol,
	}
	fields := map[string]interface{}{}
	fields["latency_ms"] = result.Latency.Milliseconds()
	fields["status_code"] = result.Code
	fields["success"] = result.Success

	p := influxdb2.NewPoint(measurement, tags, fields, time.Now())
	writeAPI.WritePoint(p)
}

// Close flushes pending writes and closes the InfluxDB client.
func Close() {
	if writeAPI != nil {
		// Flush any pending writes
		writeAPI.Flush()
	}
	if client != nil {
		client.Close()
	}
}
