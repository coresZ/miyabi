package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

type mockSubtitleManager struct {
	SubtitleManager
	searchFunc    func(ctx context.Context, code string, isUncensored bool) ([]domain.SubtitleCandidate, error)
	applyFunc     func(ctx context.Context, movieID int, candidate domain.SubtitleCandidate) (*domain.SubtitleTrack, error)
	getVTTFunc    func(ctx context.Context, id int) ([]byte, error)
	offsetFunc    func(ctx context.Context, id int, offsetMs int) error
	setDefaultFunc func(ctx context.Context, movieID int, subID int) error
	deleteFunc    func(ctx context.Context, id int) error
}

func (m *mockSubtitleManager) Search(ctx context.Context, code string, isUncensored bool) ([]domain.SubtitleCandidate, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, code, isUncensored)
	}
	return nil, nil
}

func (m *mockSubtitleManager) ApplyCandidate(ctx context.Context, movieID int, candidate domain.SubtitleCandidate) (*domain.SubtitleTrack, error) {
	if m.applyFunc != nil {
		return m.applyFunc(ctx, movieID, candidate)
	}
	return &domain.SubtitleTrack{ID: 10, DisplayName: "简体中文"}, nil
}

func (m *mockSubtitleManager) GetTrackVTT(ctx context.Context, id int) ([]byte, error) {
	if m.getVTTFunc != nil {
		return m.getVTTFunc(ctx, id)
	}
	return []byte("WEBVTT\n\n00:00:01.000 --> 00:00:04.000\nHello\n"), nil
}

func (m *mockSubtitleManager) UpdateOffset(ctx context.Context, id int, offsetMs int) error {
	if m.offsetFunc != nil {
		return m.offsetFunc(ctx, id, offsetMs)
	}
	return nil
}

func (m *mockSubtitleManager) SetDefault(ctx context.Context, movieID int, subID int) error {
	if m.setDefaultFunc != nil {
		return m.setDefaultFunc(ctx, movieID, subID)
	}
	return nil
}

func (m *mockSubtitleManager) Delete(ctx context.Context, id int) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func TestSubtitleEndpoints(t *testing.T) {
	mock := &mockSubtitleManager{}
	router := NewRouter(Dependencies{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Subtitle: mock,
	})

	t.Run("GET /api/play/subtitles/1.vtt", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/play/subtitles/1.vtt", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "text/vtt; charset=utf-8" {
			t.Errorf("expected text/vtt, got %s", rec.Header().Get("Content-Type"))
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("WEBVTT")) {
			t.Errorf("expected WEBVTT body, got %s", rec.Body.String())
		}
	})

	t.Run("GET /api/subtitles/search", func(t *testing.T) {
		mock.searchFunc = func(ctx context.Context, code string, isUncensored bool) ([]domain.SubtitleCandidate, error) {
			return []domain.SubtitleCandidate{
				{Source: "xunlei", Name: "test.srt", DisplayName: "简体中文"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/api/subtitles/search?code=ABP-123", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var results []domain.SubtitleCandidate
		if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].DisplayName != "简体中文" {
			t.Errorf("unexpected results: %+v", results)
		}
	})

	t.Run("POST /api/subtitles/apply", func(t *testing.T) {
		body := `{"movie_id": 5, "candidate": {"source": "xunlei", "name": "a.srt", "display_name": "简体中文", "url": "http://example.com/a.srt", "ext": "srt"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/subtitles/apply", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PATCH /api/subtitles/1/offset", func(t *testing.T) {
		body := `{"offset_ms": 500}`
		req := httptest.NewRequest(http.MethodPatch, "/api/subtitles/1/offset", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("PUT /api/subtitles/1/default", func(t *testing.T) {
		body := `{"movie_id": 5}`
		req := httptest.NewRequest(http.MethodPut, "/api/subtitles/1/default", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("DELETE /api/subtitles/1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/subtitles/1", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}
