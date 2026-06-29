package config

import (
	"fmt"
	"net"
	"strings"

	"GoNetWatch/internal/models"
)

var allowedTypes = map[string]bool{
	"http":      true,
	"http-head": true,
	"tcp":       true,
	"dns":       true,
}

// Validate performs semantic validation on a fully-loaded Config (after env
// overrides have been applied). All problems are collected so the operator can
// fix everything in one pass rather than restarting repeatedly.
func Validate(cfg models.Config) error {
	var msgs []string

	add := func(format string, args ...any) {
		msgs = append(msgs, fmt.Sprintf(format, args...))
	}

	// --- Targets ---------------------------------------------------------

	if len(cfg.Targets) == 0 {
		add("targets list is empty")
	} else {
		for i, t := range cfg.Targets {
			// index-only prefix for the "name is empty" error; named prefix for all others
			idxPrefix := fmt.Sprintf("target[%d]", i)
			namedPrefix := idxPrefix
			if t.Name != "" {
				namedPrefix = fmt.Sprintf("target[%d] %q", i, t.Name)
			}

			if t.Name == "" {
				add("%s has empty name", idxPrefix)
			}

			switch {
			case t.Type == "":
				add("%s has empty type", namedPrefix)
			case !allowedTypes[t.Type]:
				add("%s: unsupported protocol %q", namedPrefix, t.Type)
			}

			if t.Address == "" {
				add("%s has empty address", namedPrefix)
			} else {
				switch t.Type {
				case "http", "http-head":
					if !strings.HasPrefix(t.Address, "http://") &&
						!strings.HasPrefix(t.Address, "https://") {
						add("%s: http address must start with http:// or https://", namedPrefix)
					}
				case "tcp":
					if _, _, err := net.SplitHostPort(t.Address); err != nil {
						add("%s: tcp address must be in host:port format", namedPrefix)
					}
				case "dns":
					if strings.HasPrefix(t.Address, "http://") || strings.HasPrefix(t.Address, "https://") {
						add("%s: dns address must be a bare domain name, not a URL", namedPrefix)
					}
					if t.Resolver != "" {
						if _, _, err := net.SplitHostPort(t.Resolver); err != nil {
							add("%s: resolver must be in host:port format", namedPrefix)
						}
					}
				}
			}

			if t.IntervalSec <= 0 {
				add("%s: interval_sec must be greater than 0", namedPrefix)
			}
			if t.TimeoutSec < 0 {
				add("%s: timeout_sec must not be negative", namedPrefix)
			}
			if t.Retries < 0 {
				add("%s: retries must not be negative", namedPrefix)
			}
			if t.RetryDelayMs < 0 {
				add("%s: retry_delay_ms must not be negative", namedPrefix)
			}
		}
	}

	// --- InfluxDB (optional; skip all checks when URL is unset) ----------

	if cfg.InfluxDB.URL != "" {
		if cfg.InfluxDB.Org == "" {
			add("influxdb.org is empty")
		}
		if cfg.InfluxDB.Bucket == "" {
			add("influxdb.bucket is empty")
		}
		// Token may arrive via INFLUX_TOKEN env var; by the time Validate is
		// called, LoadConfig has already applied the override, so an empty
		// token here is a genuine misconfiguration.
		if cfg.InfluxDB.Token == "" {
			add("influxdb.token is empty (set INFLUX_TOKEN env var or add it to config)")
		}
	}

	// --- Telegram (fully optional; no validation required) ---------------

	if len(msgs) == 0 {
		return nil
	}
	if len(msgs) == 1 {
		return fmt.Errorf("config validation failed: %s", msgs[0])
	}
	return fmt.Errorf("config validation failed (%d errors):\n  - %s",
		len(msgs), strings.Join(msgs, "\n  - "))
}
