package subtitle

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	aggregator := NewAggregator(nil)
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

	vttBytes, err := svc.GetTrackVTT(context.Background(), track.ID)
	if err != nil {
		t.Fatalf("get track vtt failed: %v", err)
	}
	if !strings.HasPrefix(string(vttBytes), "WEBVTT") {
		t.Errorf("expected WebVTT content, got: %s", string(vttBytes))
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

	vttBytes, err := svc.GetTrackVTT(context.Background(), track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vttBytes), "00:00:01.500 --> 00:00:04.500") {
		t.Errorf("offset not applied, got: %s", string(vttBytes))
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
