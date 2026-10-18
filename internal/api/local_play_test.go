package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

type localPlayerStub struct {
	path string
	err  error
}

func (stub *localPlayerStub) LocalPlayFile(context.Context, string) (*os.File, string, error) {
	if stub.err != nil {
		return nil, "", stub.err
	}
	file, err := os.Open(stub.path)
	if err != nil {
		return nil, "", err
	}
	return file, filepath.Base(stub.path), nil
}

func localPlayRouter(player LocalPlayer) http.Handler {
	return NewRouter(Dependencies{
		Access:    NewAccessGateService("", ""),
		LocalPlay: player,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestLocalPlayHandlerServesRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/local/play/local-1", nil)
	request.Header.Set("Range", "bytes=2-5")
	recorder := httptest.NewRecorder()
	localPlayRouter(&localPlayerStub{path: path}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("status=%d", recorder.Code)
	}
	if recorder.Body.String() != "2345" {
		t.Fatalf("body=%q", recorder.Body.String())
	}
	if recorder.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("accept-ranges=%q", recorder.Header().Get("Accept-Ranges"))
	}
}

func TestLocalPlayHandlerAnswersHead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodHead, "/api/local/play/local-1", nil)
	recorder := httptest.NewRecorder()
	localPlayRouter(&localPlayerStub{path: path}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Length") != "10" {
		t.Fatalf("content-length=%q", recorder.Header().Get("Content-Length"))
	}
}

func TestLocalPlayHandlerMapsMissingFile(t *testing.T) {
	player := &localPlayerStub{err: domain.E(domain.KindNotFound, "本地视频不存在", nil)}
	request := httptest.NewRequest(http.MethodGet, "/api/local/play/missing", nil)
	recorder := httptest.NewRecorder()
	localPlayRouter(player).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.Code)
	}
}
