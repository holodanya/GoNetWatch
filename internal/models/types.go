package models

import "time"

// MonitorResult holds the result of a single monitor check
type MonitorResult struct {
	Target  string        `json:"target"`
	Status  string        `json:"status"`
	Code    int           `json:"code"`
	Latency time.Duration `json:"latency"`
	Error   string        `json:"error"`
	Success bool          `json:"success"`
}

// Target represents a single endpoint to monitor
type Target struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
}

// Config represents the JSON configuration file structure
type Config struct {
	HTTP []string `json:"http"`
	TCP  []string `json:"tcp"`
}
