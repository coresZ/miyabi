package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Runtime defines tunable constants and operational limits across the system.
type Runtime struct {
	// Task pool workers (concurrency)
	TaskPoolWorkers int

	// Periodic intervals
	OfflineSyncInterval  time.Duration
	MonitorCheckInterval time.Duration

	// Upstream rate limits & timeouts
	PanRateLimit     time.Duration
	PanTimeout       time.Duration
	DriveAuthTimeout time.Duration
	OfflineTimeout   time.Duration

	// Playback
	PlaybackSessionTTL time.Duration

	// In-memory caches
	CatalogueListCapacity    int
	CatalogueListTTL         time.Duration
	CatalogueDetailCapacity  int
	CatalogueDetailTTL       time.Duration
	CatalogueTagsCapacity    int
	CatalogueTagsTTL         time.Duration
	CatalogueMagnetsCapacity int
	CatalogueMagnetsTTL      time.Duration

	// Media scanning & sidecars
	MinVideoSize       int64
	VideoExtensionsRaw string
	NFOMaxSize         int64
	CoverMaxSize       int64

	// External domains
	JavBusBaseURL string
}

// DefaultRuntime returns the production defaults for all runtime parameters.
func DefaultRuntime() Runtime {
	return Runtime{
		TaskPoolWorkers:          1,
		OfflineSyncInterval:      30 * time.Second,
		MonitorCheckInterval:     5 * time.Minute,
		PanRateLimit:             500 * time.Millisecond,
		PanTimeout:               35 * time.Second,
		DriveAuthTimeout:         45 * time.Second,
		OfflineTimeout:           2 * time.Minute,
		PlaybackSessionTTL:       8 * time.Hour,
		CatalogueListCapacity:    128,
		CatalogueListTTL:         time.Minute,
		CatalogueDetailCapacity:  256,
		CatalogueDetailTTL:       5 * time.Minute,
		CatalogueTagsCapacity:    5,
		CatalogueTagsTTL:         24 * time.Hour,
		CatalogueMagnetsCapacity: 64,
		CatalogueMagnetsTTL:      time.Minute,
		MinVideoSize:             100 << 20, // 100MB
		NFOMaxSize:               2 << 20,   // 2MB
		CoverMaxSize:             32 << 20,  // 32MB
		JavBusBaseURL:            "https://www.javbus.com",
	}
}

// LoadRuntime loads defaults and overrides them from MIYABI_* environment variables.
func LoadRuntime() Runtime {
	rt := DefaultRuntime()

	if val := os.Getenv("MIYABI_TASK_POOL_WORKERS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			rt.TaskPoolWorkers = n
		}
	}
	if val := os.Getenv("MIYABI_OFFLINE_SYNC_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			rt.OfflineSyncInterval = d
		}
	}
	if val := os.Getenv("MIYABI_MONITOR_CHECK_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			rt.MonitorCheckInterval = d
		}
	}
	if val := os.Getenv("MIYABI_PAN_RATE_LIMIT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			rt.PanRateLimit = d
		}
	}
	if val := os.Getenv("MIYABI_PAN_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			rt.PanTimeout = d
		}
	}
	if val := os.Getenv("MIYABI_DRIVE_AUTH_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			rt.DriveAuthTimeout = d
		}
	}
	if val := os.Getenv("MIYABI_OFFLINE_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			rt.OfflineTimeout = d
		}
	}
	if val := os.Getenv("MIYABI_PLAYBACK_SESSION_TTL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			rt.PlaybackSessionTTL = d
		}
	}
	if val := os.Getenv("MIYABI_MIN_VIDEO_SIZE"); val != "" {
		if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
			rt.MinVideoSize = n
		}
	}
	if val := os.Getenv("MIYABI_VIDEO_EXTENSIONS"); val != "" {
		rt.VideoExtensionsRaw = strings.TrimSpace(val)
	}
	if val := os.Getenv("MIYABI_NFO_MAX_SIZE"); val != "" {
		if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
			rt.NFOMaxSize = n
		}
	}
	if val := os.Getenv("MIYABI_COVER_MAX_SIZE"); val != "" {
		if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
			rt.CoverMaxSize = n
		}
	}
	if val := os.Getenv("MIYABI_JAVBUS_BASE_URL"); val != "" {
		rt.JavBusBaseURL = strings.TrimSpace(val)
	}

	return rt
}

// VideoExtensions returns the list of video file extensions.
func (r Runtime) VideoExtensions() []string {
	if r.VideoExtensionsRaw == "" {
		return []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".ts", ".m2ts", ".mts", ".mpg", ".mpeg", ".vob"}
	}
	parts := strings.Split(r.VideoExtensionsRaw, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			if !strings.HasPrefix(trimmed, ".") {
				trimmed = "." + trimmed
			}
			result = append(result, strings.ToLower(trimmed))
		}
	}
	return result
}
