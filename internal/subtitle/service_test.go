package subtitle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/pan"
)

func setupTestService(t *testing.T) (*Service, int) {
	t.Helper()
	store, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	movie, err := store.Client.Movie.Create().SetCode("ABP-123").Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	aggregator := NewAggregator(nil, WithAllowLoopbackForTesting(true))
	svc, err := NewService(store.Client, aggregator, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return svc, movie.ID
}

func TestServiceApplyAndList(t *testing.T) {
	svc, movieID := setupTestService(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:04,000\nHello world\n"))
	}))
	defer ts.Close()

	candidate := domain.SubtitleCandidate{
		Source:      "xunlei",
		Name:        "ABP-123.chs.srt",
		DisplayName: "简体中文",
		Language:    "zh-CN",
		Version:     "standard",
		URL:         ts.URL,
		Ext:         "srt",
		Score:       100,
	}

	track, err := svc.ApplyCandidate(context.Background(), movieID, candidate)
	if err != nil {
		t.Fatalf("apply candidate failed: %v", err)
	}

	if track.DisplayName != "简体中文" || !track.IsDefault {
		t.Errorf("unexpected track: %+v", track)
	}

	list, err := svc.ListByMovie(context.Background(), movieID)
	if err != nil {
		t.Fatalf("list subtitles failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 subtitle, got %d", len(list))
	}

	vttBytes, err := svc.GetTrackVTT(context.Background(), track.ID, nil)
	if err != nil {
		t.Fatalf("get track vtt failed: %v", err)
	}
	if !strings.HasPrefix(string(vttBytes), "WEBVTT") {
		t.Errorf("expected WebVTT content, got: %s", string(vttBytes))
	}

	// Apply another candidate with the SAME language and version
	// It MUST replace the existing track and keep total count as 1.
	candidate2 := candidate
	candidate2.Source = "subtitlecat"
	track2, err := svc.ApplyCandidate(context.Background(), movieID, candidate2)
	if err != nil {
		t.Fatalf("apply second candidate failed: %v", err)
	}

	list2, err := svc.ListByMovie(context.Background(), movieID)
	if err != nil {
		t.Fatalf("list subtitles failed: %v", err)
	}
	if len(list2) != 1 {
		t.Fatalf("expected 1 subtitle after replacement, got %d", len(list2))
	}
	if list2[0].ID != track2.ID {
		t.Errorf("expected new track ID %d, got %d", track2.ID, list2[0].ID)
	}
}

func TestServiceOffset(t *testing.T) {
	svc, movieID := setupTestService(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:04,000\nHello\n"))
	}))
	defer ts.Close()

	track, err := svc.ApplyCandidate(context.Background(), movieID, domain.SubtitleCandidate{
		Source:      "xunlei",
		Name:        "test.srt",
		DisplayName: "简体中文",
		Language:    "zh-CN",
		Version:     "standard",
		URL:         ts.URL,
		Ext:         "srt",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.UpdateOffset(context.Background(), track.ID, 500); err != nil {
		t.Fatal(err)
	}

	vttBytes, err := svc.GetTrackVTT(context.Background(), track.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vttBytes), "00:00:01.500 --> 00:00:04.500") {
		t.Errorf("offset not applied, got: %s", string(vttBytes))
	}

	// Test offset override
	override := 1000
	overrideBytes, err := svc.GetTrackVTT(context.Background(), track.ID, &override)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(overrideBytes), "00:00:02.000 --> 00:00:05.000") {
		t.Errorf("offset override not applied, got: %s", string(overrideBytes))
	}
}

func TestServiceSetDefaultAndToggle(t *testing.T) {
	svc, movieID := setupTestService(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:04,000\nTest\n"))
	}))
	defer ts.Close()

	t1, err := svc.ApplyCandidate(context.Background(), movieID, domain.SubtitleCandidate{
		Source:      "xunlei",
		Name:        "1.srt",
		DisplayName: "简体中文",
		Language:    "zh-CN",
		Version:     "standard",
		URL:         ts.URL,
		Ext:         "srt",
	})
	if err != nil {
		t.Fatal(err)
	}

	t2, err := svc.ApplyCandidate(context.Background(), movieID, domain.SubtitleCandidate{
		Source:      "subtitlecat",
		Name:        "2.srt",
		DisplayName: "繁体中文（无码版）",
		Language:    "zh-TW",
		Version:     "uncensored",
		URL:         ts.URL,
		Ext:         "srt",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.SetDefault(context.Background(), movieID, t1.ID); err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListByMovie(context.Background(), movieID)
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range list {
		if item.ID == t1.ID && !item.IsDefault {
			t.Errorf("expected t1 to be default")
		}
		if item.ID == t2.ID && item.IsDefault {
			t.Errorf("expected t2 not to be default")
		}
	}
}

func TestServiceIndexLocalSubtitle(t *testing.T) {
	svc, movieID := setupTestService(t)

	err := svc.IndexLocalSubtitle(context.Background(), movieID, pan.File{
		ID:       "file-123",
		PickCode: "pick-123",
		Name:     "ABP-123.uncensored.chs.srt",
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListByMovie(context.Background(), movieID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 subtitle, got %d", len(list))
	}
	if list[0].DisplayName != "简体中文（无码版）" {
		t.Errorf("expected '简体中文（无码版）', got %q", list[0].DisplayName)
	}
	if list[0].Source != "local" {
		t.Errorf("expected source 'local', got %q", list[0].Source)
	}
}

func TestAggregatorSSRFBlocked(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"))
	}))
	defer ts.Close()

	// Default aggregator without allowLoopback
	agg := NewAggregator(nil)
	_, err := agg.DownloadAndConvert(context.Background(), Candidate{
		URL: ts.URL,
		Ext: "srt",
	})
	if err == nil {
		t.Fatalf("expected SSRF error when accessing loopback server, got nil")
	}
	if !strings.Contains(err.Error(), "prohibited") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConvertToWebVTT_RejectsInvalidContent(t *testing.T) {
	invalidContents := []struct {
		name    string
		content string
		ext     string
	}{
		{"json error", `{"status": 500, "message": "internal server error"}`, "srt"},
		{"html error", `<!DOCTYPE html><html><body>Access Denied</body></html>`, "srt"},
		{"empty content", ``, "srt"},
		{"arbitrary text without cues", `this is just some plain text without any timestamps`, "vtt"},
	}

	for _, tt := range invalidContents {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ConvertToWebVTT(tt.content, tt.ext)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestServiceDelete_SharedFileRefCounts(t *testing.T) {
	svc, movieID := setupTestService(t)
	ctx := context.Background()

	// Create a shared dummy cache file
	sharedPath := filepath.Join(svc.cacheDir, "shared.vtt")
	if err := os.WriteFile(sharedPath, []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nShared\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create track 1 pointing to sharedPath
	t1, err := svc.db.Subtitle.Create().
		SetMovieID(movieID).
		SetName("sub1.vtt").
		SetDisplayName("Track 1").
		SetLanguage("zh-CN").
		SetFormat("vtt").
		SetVersionTag("standard").
		SetSource("local").
		SetStoragePath(sharedPath).
		SetOffsetMs(0).
		SetIsDefault(true).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Create track 2 pointing to the same sharedPath
	t2, err := svc.db.Subtitle.Create().
		SetMovieID(movieID).
		SetName("sub2.vtt").
		SetDisplayName("Track 2").
		SetLanguage("zh-TW").
		SetFormat("vtt").
		SetVersionTag("standard").
		SetSource("local").
		SetStoragePath(sharedPath).
		SetOffsetMs(0).
		SetIsDefault(false).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Delete track 1
	if err := svc.Delete(ctx, t1.ID); err != nil {
		t.Fatalf("delete track 1 failed: %v", err)
	}

	// File MUST still exist because track 2 references it!
	if _, err := os.Stat(sharedPath); os.IsNotExist(err) {
		t.Fatalf("shared file was incorrectly deleted while track 2 still references it")
	}

	// 2. Delete track 2
	if err := svc.Delete(ctx, t2.ID); err != nil {
		t.Fatalf("delete track 2 failed: %v", err)
	}

	// Now file MUST be deleted because refcount reached 0
	if _, err := os.Stat(sharedPath); !os.IsNotExist(err) {
		t.Fatalf("shared file should be removed after all referencing tracks are deleted")
	}
}


