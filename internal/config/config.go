package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"

	"GoNetWatch/internal/models"
)

// LoadConfig reads and parses the YAML configuration from the given path.
// It also checks for environment variables to override sensitive values like the InfluxDB token
// and Telegram credentials.
func LoadConfig(path string) (models.Config, error) {
	var cfg models.Config

	// Load environment variables from .env file if it exists
	// Ignore error if .env file doesn't exist (e.g., in production Docker environments)
	_ = godotenv.Load()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config file: %w", err)
	}

	// Override InfluxDB token from environment variable if it exists
	if token := os.Getenv("INFLUX_TOKEN"); token != "" {
		cfg.InfluxDB.Token = token
	}

	// Override Telegram configuration from environment variables if they exist
	if botToken := os.Getenv("TELEGRAM_BOT_TOKEN"); botToken != "" {
		cfg.Telegram.BotToken = botToken
	}

	// Override Telegram chat IDs from environment variable (comma-separated)
	if chatIDsStr := os.Getenv("TELEGRAM_CHAT_IDS"); chatIDsStr != "" {
		chatIDsList := strings.Split(chatIDsStr, ",")
		// Trim whitespace from each chat ID
		for i, id := range chatIDsList {
			chatIDsList[i] = strings.TrimSpace(id)
		}
		cfg.Telegram.ChatIDs = chatIDsList
	}

	return cfg, nil
}
