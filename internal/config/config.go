package config

import (
	"encoding/json"
	"fmt"
	"os"

	"GoNetWatch/internal/models"
)

// LoadConfig reads and parses the JSON configuration from the given path.
func LoadConfig(path string) (models.Config, error) {
	var cfg models.Config

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config file: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}
