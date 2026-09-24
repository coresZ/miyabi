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

	"github.com/ppxb/miyabi/internal/library/scan"
)

type stubLocalScanLibrary struct {
	LibraryManager
	scannedPath string
	result      *scan.LocalScanResult
	err         error
}

func (s *stubLocalScanLibrary) ScanLocal(_ context.Context, path string) (*scan.LocalScanResult, error) {
	s.scannedPath = path
	return s.result, s.err
}

func TestLibraryLocalScanHandler(t *testing.T) {
	lib := &stubLocalScanLibrary{
		result: &scan.LocalScanResult{
			FilesScanned: 10,
			MediaFiles:   5,
			MoviesAdded:  5,
			NFORead:      5,
		},
	}
	router := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Library: lib,
		EmbyDir: "/default/emby/dir",
	})

	// 1. Explicit path
	body, _ := json.Marshal(map[string]string{"path": "/custom/path"})
	req := httptest.NewRequest(http.MethodPost, "/api/library/scan/local", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if lib.scannedPath != "/custom/path" {
		t.Fatalf("expected scanned path /custom/path, got %s", lib.scannedPath)
	}

	var res scan.LocalScanResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.MediaFiles != 5 || res.MoviesAdded != 5 {
		t.Fatalf("unexpected response body: %+v", res)
	}

	// 2. Default path fallback
	req2 := httptest.NewRequest(http.MethodPost, "/api/library/scan/local", bytes.NewReader([]byte("{}")))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if lib.scannedPath != "/default/emby/dir" {
		t.Fatalf("expected default path /default/emby/dir, got %s", lib.scannedPath)
	}
}
