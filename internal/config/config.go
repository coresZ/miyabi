package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/ppxb/miyabi/internal/logging"
)

type Config struct {
	Listen         string
	DataDir        string
	EmbyDir        string
	PublicURL      string
	STRMToken      string
	LogLevel       string
	AccessPassword string
	Runtime        Runtime
}

func Load() (Config, error) {
	listen := envOrDefault("MIYABI_LISTEN", ":8080")
	dataDir := envOrDefault("MIYABI_DATA_DIR", "./data")
	embyDir := strings.TrimSpace(os.Getenv("MIYABI_EMBY_DIR"))
	if embyDir == "" {
		embyDir = filepath.Join(dataDir, "emby")
	}
	publicURL := strings.TrimRight(strings.TrimSpace(os.Getenv("MIYABI_PUBLIC_URL")), "/")
	if publicURL == "" {
		port := "8080"
		if _, p, err := net.SplitHostPort(strings.TrimSpace(listen)); err == nil && p != "" {
			port = p
		}
		publicURL = "http://127.0.0.1:" + port
	}
	cfg := Config{
		Listen:         listen,
		DataDir:        dataDir,
		EmbyDir:        embyDir,
		PublicURL:      publicURL,
		STRMToken:      strings.TrimSpace(os.Getenv("MIYABI_STRM_TOKEN")),
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
	cfg.EmbyDir = strings.TrimSpace(cfg.EmbyDir)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))

	if cfg.Listen == "" {
		return errors.New("MIYABI_LISTEN must not be empty")
	}
	if cfg.DataDir == "" {
		return errors.New("MIYABI_DATA_DIR must not be empty")
	}
	if cfg.EmbyDir == "" {
		return errors.New("MIYABI_EMBY_DIR must not be empty")
	}
	if err := logging.Validate(cfg.LogLevel); err != nil {
		return fmt.Errorf("MIYABI_LOG_LEVEL %w", err)
	}
	return nil
}
