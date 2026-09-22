package config

import (
	"slices"
	"testing"
	"time"
)

func TestRuntimeOverridesAndExtensions(t *testing.T) {
	t.Run("default runtime values", func(t *testing.T) {
		rt := DefaultRuntime()
		if rt.TaskPoolWorkers != 1 {
			t.Fatalf("expected 1 worker, got %d", rt.TaskPoolWorkers)
		}
		if rt.OfflineSyncInterval != 30*time.Second {
			t.Fatalf("expected 30s offline sync, got %v", rt.OfflineSyncInterval)
		}
		if rt.MonitorCheckInterval != 5*time.Minute {
			t.Fatalf("expected 5m monitor interval, got %v", rt.MonitorCheckInterval)
		}
		if rt.PanRateLimit != 500*time.Millisecond {
			t.Fatalf("expected 500ms pan rate limit, got %v", rt.PanRateLimit)
		}
		if rt.PlaybackSessionTTL != 8*time.Hour {
			t.Fatalf("expected 8h session TTL, got %v", rt.PlaybackSessionTTL)
		}
		exts := rt.VideoExtensions()
		if !slices.Contains(exts, ".mp4") || !slices.Contains(exts, ".mkv") {
			t.Fatalf("expected default video extensions, got %v", exts)
		}
	})

	t.Run("environment overrides runtime", func(t *testing.T) {
		t.Setenv("MIYABI_TASK_POOL_WORKERS", "4")
		t.Setenv("MIYABI_OFFLINE_SYNC_INTERVAL", "15s")
		t.Setenv("MIYABI_MONITOR_CHECK_INTERVAL", "2m")
		t.Setenv("MIYABI_PAN_RATE_LIMIT", "250ms")
		t.Setenv("MIYABI_PAN_TIMEOUT", "40s")
		t.Setenv("MIYABI_DRIVE_AUTH_TIMEOUT", "60s")
		t.Setenv("MIYABI_OFFLINE_TIMEOUT", "3m")
		t.Setenv("MIYABI_PLAYBACK_SESSION_TTL", "12h")
		t.Setenv("MIYABI_MIN_VIDEO_SIZE", "52428800")
		t.Setenv("MIYABI_VIDEO_EXTENSIONS", "mp4, mkv, avi")
		t.Setenv("MIYABI_NFO_MAX_SIZE", "1048576")
		t.Setenv("MIYABI_COVER_MAX_SIZE", "16777216")
		t.Setenv("MIYABI_JAVBUS_BASE_URL", "https://custom.javbus.example")

		rt := LoadRuntime()
		if rt.TaskPoolWorkers != 4 {
			t.Fatalf("expected 4 workers, got %d", rt.TaskPoolWorkers)
		}
		if rt.OfflineSyncInterval != 15*time.Second {
			t.Fatalf("expected 15s offline sync, got %v", rt.OfflineSyncInterval)
		}
		if rt.MonitorCheckInterval != 2*time.Minute {
			t.Fatalf("expected 2m monitor interval, got %v", rt.MonitorCheckInterval)
		}
		if rt.PanRateLimit != 250*time.Millisecond {
			t.Fatalf("expected 250ms pan rate limit, got %v", rt.PanRateLimit)
		}
		if rt.PanTimeout != 40*time.Second {
			t.Fatalf("expected 40s pan timeout, got %v", rt.PanTimeout)
		}
		if rt.DriveAuthTimeout != 60*time.Second {
			t.Fatalf("expected 60s drive auth timeout, got %v", rt.DriveAuthTimeout)
		}
		if rt.OfflineTimeout != 3*time.Minute {
			t.Fatalf("expected 3m offline timeout, got %v", rt.OfflineTimeout)
		}
		if rt.PlaybackSessionTTL != 12*time.Hour {
			t.Fatalf("expected 12h playback session TTL, got %v", rt.PlaybackSessionTTL)
		}
		if rt.MinVideoSize != 52428800 {
			t.Fatalf("expected 52428800 min video size, got %d", rt.MinVideoSize)
		}
		if rt.NFOMaxSize != 1048576 {
			t.Fatalf("expected 1048576 NFO max size, got %d", rt.NFOMaxSize)
		}
		if rt.CoverMaxSize != 16777216 {
			t.Fatalf("expected 16777216 cover max size, got %d", rt.CoverMaxSize)
		}
		if rt.JavBusBaseURL != "https://custom.javbus.example" {
			t.Fatalf("expected custom JavBus URL, got %s", rt.JavBusBaseURL)
		}
		exts := rt.VideoExtensions()
		expectedExts := []string{".mp4", ".mkv", ".avi"}
		if !slices.Equal(exts, expectedExts) {
			t.Fatalf("expected %v, got %v", expectedExts, exts)
		}
	})
}
