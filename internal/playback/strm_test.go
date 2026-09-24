package playback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestStreamURLPicksBestQuality(t *testing.T) {
	service, _ := playFixture(t)
	stub := stubOf(t, service.drive)

	// Set PickCode in DB for file 101
	service.database.File.Update().Where(file.FileIDEQ("101")).SetPickCode("pick-101").SaveX(t.Context())

	stub.playURL = func(ctx context.Context, token, pickCode string) ([]pan.PlaySource, error) {
		if pickCode != "pick-101" {
			t.Fatalf("unexpected pickCode: %s", pickCode)
		}
		return []pan.PlaySource{
			{URL: "https://cdn.example/720p.m3u8", Height: 720, Definition: 2},
			{URL: "https://cdn.example/1080p.m3u8", Height: 1080, Definition: 3},
			{URL: "https://cdn.example/original.mp4", Height: 1080, Definition: 100},
		}, nil
	}

	bestURL, err := service.StreamURL(t.Context(), "101")
	if err != nil {
		t.Fatalf("StreamURL failed: %v", err)
	}
	if bestURL != "https://cdn.example/original.mp4" {
		t.Fatalf("expected definition 100 original URL, got %s", bestURL)
	}
}

func TestStreamURLFallsBackToInfoWhenPickCodeMissingInDB(t *testing.T) {
	service, _ := playFixture(t)
	stub := stubOf(t, service.drive)

	// Clear PickCode in DB
	service.database.File.Update().Where(file.FileIDEQ("101")).SetPickCode("").SaveX(t.Context())

	stub.info = func(ctx context.Context, token, id string) (pan.FileInfo, error) {
		if id == "101" {
			return pan.FileInfo{File: pan.File{ID: "101", Name: "ABP-001.mp4", PickCode: "info-pick-101"}}, nil
		}
		return pan.FileInfo{}, nil
	}

	stub.playURL = func(ctx context.Context, token, pickCode string) ([]pan.PlaySource, error) {
		if pickCode != "info-pick-101" {
			t.Fatalf("unexpected pickCode from info fallback: %s", pickCode)
		}
		return []pan.PlaySource{
			{URL: "https://cdn.example/video.mp4", Height: 1080, Definition: 100},
		}, nil
	}

	bestURL, err := service.StreamURL(t.Context(), "101")
	if err != nil {
		t.Fatalf("StreamURL failed: %v", err)
	}
	if bestURL != "https://cdn.example/video.mp4" {
		t.Fatalf("expected video URL, got %s", bestURL)
	}
}

func TestStreamURLFailsWhenNoSources(t *testing.T) {
	service, _ := playFixture(t)
	stub := stubOf(t, service.drive)

	service.database.File.Update().Where(file.FileIDEQ("101")).SetPickCode("pick-101").SaveX(t.Context())
	stub.playURL = func(ctx context.Context, token, pickCode string) ([]pan.PlaySource, error) {
		return nil, nil
	}

	_, err := service.StreamURL(t.Context(), "101")
	if err == nil {
		t.Fatal("expected error when 115 returns no sources")
	}
}

func TestPlaybackOpenMedia(t *testing.T) {
	service, _ := playFixture(t)
	stub := stubOf(t, service.drive)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", "1024")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer upstream.Close()

	stub.openMedia = func(ctx context.Context, method, address string, headers http.Header) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, address, nil)
		if err != nil {
			return nil, err
		}
		return http.DefaultClient.Do(req)
	}

	resp, err := service.OpenMedia(t.Context(), http.MethodHead, upstream.URL, nil)
	if err != nil {
		t.Fatalf("OpenMedia failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("unexpected OpenMedia response: %+v", resp)
	}
}
