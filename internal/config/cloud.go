package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Cloud struct {
	ListenAddr      string
	ControlAPIToken string
	EdgeToken       string
	CommandTimeout  time.Duration
}

func LoadCloud() (Cloud, error) {
	timeout, err := durationEnv("COMMAND_TIMEOUT", 6*time.Second)
	if err != nil {
		return Cloud{}, err
	}

	listenAddr := env("CLOUD_ADDR", ":8080")
	if port := os.Getenv("PORT"); port != "" {
		listenAddr = ":" + port
	}

	cfg := Cloud{
		ListenAddr:      listenAddr,
		ControlAPIToken: os.Getenv("CONTROL_API_TOKEN"),
		EdgeToken:       os.Getenv("EDGE_TOKEN"),
		CommandTimeout:  timeout,
	}

	if cfg.ControlAPIToken == "" {
		return Cloud{}, fmt.Errorf("CONTROL_API_TOKEN is required")
	}

	if cfg.EdgeToken == "" {
		return Cloud{}, fmt.Errorf("EDGE_TOKEN is required")
	}

	return cfg, nil
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}

	return value, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}

	return value, nil
}
