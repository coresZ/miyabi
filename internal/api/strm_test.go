package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type strmPlayStub struct {
	PlayManager
	streamURL   string
	streamErr   error
	headHeaders http.Header
	headStatus  int
	headErr     error
}

func (s *strmPlayStub) StreamURL(ctx context.Context, fileID string) (string, error) {
	if s.streamErr != nil {
		return "", s.streamErr
	}
	return s.streamURL, nil
}

func (s *strmPlayStub) OpenMedia(ctx context.Context, method, address string, headers http.Header) (*http.Response, error) {
	if s.headErr != nil {
		return nil, s.headErr
	}
	res := &http.Response{
		StatusCode: s.headStatus,
		Header:     s.headHeaders,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	if res.StatusCode == 0 {
		res.StatusCode = http.StatusOK
	}
	return res, nil
}

func TestSTRMPlayHandlerRedirectsGET(t *testing.T) {
	stub := &strmPlayStub{
		streamURL: "https://cdn.115.com/video/original.mp4?token=sig",
	}
	router := NewRouter(Dependencies{
		Play:   stub,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/strm/play/12345", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status 302 Found, got %d", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "https://cdn.115.com/video/original.mp4?token=sig" {
		t.Fatalf("expected Location %s, got %s", stub.streamURL, location)
	}
}

func TestSTRMPlayHandlerForwardsHEAD(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Content-Type", "video/mp4")
	headers.Set("Content-Length", "104857600")
	headers.Set("Accept-Ranges", "bytes")

	stub := &strmPlayStub{
		streamURL:   "https://cdn.115.com/video/original.mp4",
		headHeaders: headers,
		headStatus:  http.StatusOK,
	}
	router := NewRouter(Dependencies{
		Play:   stub,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/api/strm/play/12345", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("expected Content-Type video/mp4, got %s", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Content-Length") != "104857600" {
		t.Fatalf("expected Content-Length 104857600, got %s", rec.Header().Get("Content-Length"))
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("expected Accept-Ranges bytes, got %s", rec.Header().Get("Accept-Ranges"))
	}
}

func TestSTRMPlayHandlerTokenAuthentication(t *testing.T) {
	stub := &strmPlayStub{
		streamURL: "https://cdn.115.com/video/original.mp4",
	}
	router := NewRouter(Dependencies{
		Play:      stub,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		STRMToken: "secret123",
	})

	// Missing token
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/strm/play/12345", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for missing token, got %d", rec.Code)
		}
	}

	// Wrong token
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/strm/play/12345?token=wrong", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for wrong token, got %d", rec.Code)
		}
	}

	// Correct token
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/strm/play/12345?token=secret123", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 for valid token, got %d", rec.Code)
		}
		if location := rec.Header().Get("Location"); location != stub.streamURL {
			t.Fatalf("expected Location %s, got %s", stub.streamURL, location)
		}
	}
}
