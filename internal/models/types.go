package models

import "time"

// MonitorResult holds the result of a single monitor check
type MonitorResult struct {
	Target   string        `yaml:"target"`
	Protocol string        `yaml:"protocol"`
	Status   string        `yaml:"status"`
	Code     int           `yaml:"code"`
	Latency  time.Duration `yaml:"latency"`
	Error    string        `yaml:"error"`
	Success  bool          `yaml:"success"`
}

// Target represents a single endpoint to monitor
type Target struct {
	// Name is a human-readable identifier for this target
	Name string `yaml:"name"`
	// Type indicates the type of check to perform: "http", "http-head", "tcp", etc.
	Type string `yaml:"type"`
	// Protocol is kept for backward compatibility; it's typically the same as Type.
	Protocol string `yaml:"protocol"`
	// Address is the target URL or host:port
	Address     string `yaml:"address"`
	IntervalSec int    `yaml:"interval_sec"` // interval in seconds between checks
}

// Config represents the YAML configuration file structure
type Config struct {
	// Targets is the list of targets to monitor. Each entry maps to models.Target
	Targets []Target `yaml:"targets"`
	// InfluxDB contains configuration for writing metrics to InfluxDB v2
	InfluxDB InfluxDBConfig `yaml:"influxdb"`
	// Telegram contains configuration for Telegram notifications
	Telegram TelegramConfig `yaml:"telegram"`
}

// InfluxDBConfig holds connection info for InfluxDB v2
type InfluxDBConfig struct {
	URL    string `yaml:"url"`
	Token  string `yaml:"token"`
	Org    string `yaml:"org"`
	Bucket string `yaml:"bucket"`
}

// TelegramConfig holds configuration for Telegram bot notifications
type TelegramConfig struct {
	BotToken string   `yaml:"bot_token"`
	ChatIDs  []string `yaml:"chat_ids"`
}
