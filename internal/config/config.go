package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"

	"GoNetWatch/internal/models"
)

// LoadConfig reads configs/config.yaml and applies env-var overrides for sensitive fields.
func LoadConfig(path string) (models.Config, error) {
	var cfg models.Config

	_ = godotenv.Load() // silently ignored when .env is absent (Docker-friendly)

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config file: %w", err)
	}

	if token := os.Getenv("INFLUX_TOKEN"); token != "" {
		cfg.InfluxDB.Token = token
	}

	if botToken := os.Getenv("TELEGRAM_BOT_TOKEN"); botToken != "" {
		cfg.Telegram.BotToken = botToken
	}

	if chatIDsStr := os.Getenv("TELEGRAM_CHAT_IDS"); chatIDsStr != "" {
		chatIDsList := strings.Split(chatIDsStr, ",")
		for i, id := range chatIDsList {
			chatIDsList[i] = strings.TrimSpace(id)
		}
		cfg.Telegram.ChatIDs = chatIDsList
	}

	return cfg, nil
}
