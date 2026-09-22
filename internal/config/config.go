package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ppxb/miyabi/internal/logging"
)

type Config struct {
	Listen         string
	DataDir        string
	LogLevel       string
	AccessPassword string
	Runtime        Runtime
}

func Load() (Config, error) {
	cfg := Config{
		Listen:         envOrDefault("MIYABI_LISTEN", ":8080"),
		DataDir:        envOrDefault("MIYABI_DATA_DIR", "./data"),
		LogLevel:       envOrDefault("MIYABI_LOG_LEVEL", "info"),
		AccessPassword: os.Getenv("MIYABI_ACCESS_PASSWORD"),
		Runtime:        DefaultRuntime(),
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value, set := os.LookupEnv(key); set {
		return value
	}
	return fallback
}

func (cfg *Config) validate() error {
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))

	if cfg.Listen == "" {
		return errors.New("MIYABI_LISTEN must not be empty")
	}
	if cfg.DataDir == "" {
		return errors.New("MIYABI_DATA_DIR must not be empty")
	}
	if err := logging.Validate(cfg.LogLevel); err != nil {
		return fmt.Errorf("MIYABI_LOG_LEVEL %w", err)
	}
	return nil
}
