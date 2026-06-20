package main

import (
	"fmt"
	"os"

	cfgpkg "GoNetWatch/internal/config"
	"GoNetWatch/internal/models"
	"GoNetWatch/internal/monitor"
)

func main() {
	cfg, err := cfgpkg.LoadConfig("configs/config.json")
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	var targets []models.Target
	for _, h := range cfg.HTTP {
		targets = append(targets, models.Target{Protocol: "http", Address: h})
	}
	for _, t := range cfg.TCP {
		targets = append(targets, models.Target{Protocol: "tcp", Address: t})
	}

	if len(targets) == 0 {
		fmt.Println("No targets found in configs/config.json")
		os.Exit(1)
	}

	monitor.MonitorTargets(targets)
}
