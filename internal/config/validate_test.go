package config

import (
	"strings"
	"testing"

	"GoNetWatch/internal/models"
)

func TestValidate(t *testing.T) {
	validHTTP := models.Target{
		Name:             "API",
		Type:             "http",
		Address:          "https://api.example.com/health",
		IntervalSec:      30,
		TimeoutSec:       5,
		Retries:          2,
		RetryDelayMs:     250,
		ExpectedStatuses: []int{200, 204},
	}

	tests := []struct {
		name           string
		cfg            models.Config
		wantErr        bool
		wantSubstrings []string
	}{
		// Verifies that a complete, valid HTTP target and InfluxDB config pass validation.
		{
			name: "happy path with http target and influxdb",
			cfg: models.Config{
				Targets: []models.Target{validHTTP},
				InfluxDB: models.InfluxDBConfig{
					URL:    "http://localhost:8086",
					Token:  "token",
					Org:    "gonetwatch",
					Bucket: "metrics",
				},
			},
		},
		// Verifies that all supported target types are accepted when their addresses are well-formed.
		{
			name: "happy path with all supported target types",
			cfg: models.Config{
				Targets: []models.Target{
					validHTTP,
					{
						Name:             "Status Page",
						Type:             "http-head",
						Address:          "https://status.example.com",
						IntervalSec:      60,
						ExpectedStatuses: []int{200, 301},
					},
					{
						Name:        "Postgres",
						Type:        "tcp",
						Address:     "db.example.com:5432",
						IntervalSec: 15,
					},
					{
						Name:        "DNS",
						Type:        "dns",
						Address:     "example.com",
						Resolver:    "1.1.1.1:53",
						IntervalSec: 20,
					},
				},
			},
		},
		// Verifies that a nil target slice is rejected instead of silently accepting an unusable config.
		{
			name: "nil targets",
			cfg: models.Config{
				Targets: nil,
			},
			wantErr:        true,
			wantSubstrings: []string{"targets list is empty"},
		},
		// Verifies that an explicitly empty target slice is rejected.
		{
			name: "empty targets",
			cfg: models.Config{
				Targets: []models.Target{},
			},
			wantErr:        true,
			wantSubstrings: []string{"targets list is empty"},
		},
		// Verifies that missing required target fields are all reported.
		{
			name: "missing target fields",
			cfg: models.Config{
				Targets: []models.Target{
					{},
				},
			},
			wantErr: true,
			wantSubstrings: []string{
				`target[0] has empty name`,
				`target[0] has empty type`,
				`target[0] has empty address`,
				`target[0]: interval_sec must be greater than 0`,
			},
		},
		// Verifies that unsupported protocols are rejected.
		{
			name: "unsupported target type",
			cfg: models.Config{
				Targets: []models.Target{
					{
						Name:        "Ping",
						Type:        "icmp",
						Address:     "example.com",
						IntervalSec: 10,
					},
				},
			},
			wantErr:        true,
			wantSubstrings: []string{`target[0] "Ping": unsupported protocol "icmp"`},
		},
		// Verifies that HTTP targets require an http:// or https:// URL.
		{
			name: "invalid http url format",
			cfg: models.Config{
				Targets: []models.Target{
					{
						Name:        "API",
						Type:        "http",
						Address:     "api.example.com/health",
						IntervalSec: 10,
					},
				},
			},
			wantErr:        true,
			wantSubstrings: []string{`target[0] "API": http address must start with http:// or https://`},
		},
		// Verifies that TCP targets must use host:port form.
		{
			name: "invalid tcp address format",
			cfg: models.Config{
				Targets: []models.Target{
					{
						Name:        "Postgres",
						Type:        "tcp",
						Address:     "db.example.com",
						IntervalSec: 10,
					},
				},
			},
			wantErr:        true,
			wantSubstrings: []string{`target[0] "Postgres": tcp address must be in host:port format`},
		},
		// Verifies that DNS targets accept bare domain names, not URL strings.
		{
			name: "invalid dns address url",
			cfg: models.Config{
				Targets: []models.Target{
					{
						Name:        "DNS",
						Type:        "dns",
						Address:     "https://example.com",
						IntervalSec: 10,
					},
				},
			},
			wantErr:        true,
			wantSubstrings: []string{`target[0] "DNS": dns address must be a bare domain name, not a URL`},
		},
		// Verifies that a malformed DNS resolver is reported.
		{
			name: "invalid dns resolver",
			cfg: models.Config{
				Targets: []models.Target{
					{
						Name:        "DNS",
						Type:        "dns",
						Address:     "example.com",
						Resolver:    "1.1.1.1",
						IntervalSec: 10,
					},
				},
			},
			wantErr:        true,
			wantSubstrings: []string{`target[0] "DNS": resolver must be in host:port format`},
		},
		// Verifies that negative or zero timing and retry values are rejected.
		{
			name: "invalid interval timeout and retry values",
			cfg: models.Config{
				Targets: []models.Target{
					{
						Name:         "API",
						Type:         "http",
						Address:      "https://api.example.com",
						IntervalSec:  -1,
						TimeoutSec:   -2,
						Retries:      -3,
						RetryDelayMs: -4,
					},
				},
			},
			wantErr: true,
			wantSubstrings: []string{
				`target[0] "API": interval_sec must be greater than 0`,
				`target[0] "API": timeout_sec must not be negative`,
				`target[0] "API": retries must not be negative`,
				`target[0] "API": retry_delay_ms must not be negative`,
			},
		},
		// Verifies that expected_statuses is HTTP-only and that status codes must be valid HTTP codes.
		{
			name: "invalid expected statuses",
			cfg: models.Config{
				Targets: []models.Target{
					{
						Name:             "Postgres",
						Type:             "tcp",
						Address:          "db.example.com:5432",
						IntervalSec:      10,
						ExpectedStatuses: []int{99, 600},
					},
				},
			},
			wantErr: true,
			wantSubstrings: []string{
				`target[0] "Postgres": expected_statuses is only allowed for http and http-head targets`,
				`target[0] "Postgres": expected_statuses contains invalid code 99 (must be 100..599)`,
				`target[0] "Postgres": expected_statuses contains invalid code 600 (must be 100..599)`,
			},
		},
		// Verifies that partially configured InfluxDB settings are treated as malformed config.
		{
			name: "malformed influxdb configuration",
			cfg: models.Config{
				Targets: []models.Target{validHTTP},
				InfluxDB: models.InfluxDBConfig{
					URL: "http://localhost:8086",
				},
			},
			wantErr: true,
			wantSubstrings: []string{
				"influxdb.org is empty",
				"influxdb.bucket is empty",
				"influxdb.token is empty",
			},
		},
		// Verifies that validation collects multiple independent target errors in one response.
		{
			name: "multiple target errors are aggregated",
			cfg: models.Config{
				Targets: []models.Target{
					{
						Name:        "API",
						Type:        "http",
						Address:     "api.example.com",
						IntervalSec: 0,
					},
					{
						Name:        "Cache",
						Type:        "tcp",
						Address:     "cache.example.com",
						IntervalSec: 10,
					},
				},
			},
			wantErr: true,
			wantSubstrings: []string{
				"config validation failed (3 errors):",
				`target[0] "API": http address must start with http:// or https://`,
				`target[0] "API": interval_sec must be greater than 0`,
				`target[1] "Cache": tcp address must be in host:port format`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.cfg)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() error = nil, want an error containing %q", tt.wantSubstrings)
				}
				for _, want := range tt.wantSubstrings {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("Validate() error = %q, want substring %q", err.Error(), want)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
