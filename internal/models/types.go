package models

import "time"

// MonitorResult carries the outcome of a single check attempt.
type MonitorResult struct {
	// Identity — stamped by checkTarget from the Target config.
	Name     string `yaml:"name"`     // human-readable name (InfluxDB "target" tag)
	Target   string `yaml:"target"`   // technical address: URL, host:port, or domain
	Protocol string `yaml:"protocol"` // check type: http, http-head, tcp, dns
	Resolver string `yaml:"resolver"` // DNS resolver used (dns type only)

	// Measurement data
	Status        string        `yaml:"status"`
	Code          int           `yaml:"code"`
	Latency       time.Duration `yaml:"latency"`
	Error         string        `yaml:"error"`
	Success       bool          `yaml:"success"`
	Attempts      int           `yaml:"attempts"`
	ResolvedCount int           `yaml:"resolved_count"` // IPs resolved (dns type only)
}

// Target represents a single monitoring check defined in config.yaml.
type Target struct {
	Name         string `yaml:"name"`
	Type         string `yaml:"type"`
	Protocol     string `yaml:"protocol"` // equals Type in practice; kept for YAML backward-compat
	Address      string `yaml:"address"`
	IntervalSec  int    `yaml:"interval_sec"`   // seconds between checks
	TimeoutSec   int    `yaml:"timeout_sec"`    // 0 = protocol default (http:5s, tcp/dns:3s)
	Retries      int    `yaml:"retries"`        // additional attempts on failure; 0 = no retry
	RetryDelayMs int    `yaml:"retry_delay_ms"` // ms between retries; 0 = default 300ms
	Resolver     string `yaml:"resolver"`       // DNS server as host:port (dns type only)
}

// Config is the root structure of configs/config.yaml.
type Config struct {
	Targets  []Target       `yaml:"targets"`
	InfluxDB InfluxDBConfig `yaml:"influxdb"`
	Telegram TelegramConfig `yaml:"telegram"`
}

// InfluxDBConfig holds connection parameters for InfluxDB v2.
type InfluxDBConfig struct {
	URL    string `yaml:"url"`
	Token  string `yaml:"token"`
	Org    string `yaml:"org"`
	Bucket string `yaml:"bucket"`
}

// TelegramConfig holds Telegram bot notification parameters.
type TelegramConfig struct {
	BotToken string   `yaml:"bot_token"`
	ChatIDs  []string `yaml:"chat_ids"`
}
