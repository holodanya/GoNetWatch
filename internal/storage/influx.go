package storage

import (
	"log/slog"
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
		return nil
	}

	client = influxdb2.NewClient(cfg.URL, cfg.Token)
	writeAPI = client.WriteAPI(cfg.Org, cfg.Bucket)

	errCh = writeAPI.Errors()
	go func() {
		for err := range errCh {
			slog.Error("InfluxDB write error", slog.String("error", err.Error()))
		}
	}()

	return nil
}

// WriteMetric writes a single MonitorResult to InfluxDB asynchronously.
// Non-blocking (uses WriteAPI). No-op if Init was not called.
func WriteMetric(result models.MonitorResult) {
	if writeAPI == nil {
		return
	}

	targetName := result.Name
	if targetName == "" {
		targetName = result.Target
	}
	tags := map[string]string{
		"target":   targetName,
		"address":  result.Target,
		"protocol": result.Protocol,
	}
	if result.Protocol == "dns" && result.Resolver != "" {
		tags["resolver"] = result.Resolver
	}

	fields := map[string]interface{}{
		"success":    result.Success,
		"latency_ms": result.Latency.Milliseconds(),
		"attempts":   result.Attempts,
	}
	if result.Code > 0 {
		fields["status_code"] = result.Code
	}
	if result.Protocol == "dns" {
		fields["resolved_count"] = result.ResolvedCount
	}

	p := influxdb2.NewPoint("network_latency", tags, fields, time.Now())
	writeAPI.WritePoint(p)
}

// Close flushes pending writes and closes the InfluxDB client.
func Close() {
	if writeAPI != nil {
		writeAPI.Flush()
	}
	if client != nil {
		client.Close()
	}
}
