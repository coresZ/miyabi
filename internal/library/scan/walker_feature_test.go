package scan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/pan"
)

func TestFastSTRMGeneration(t *testing.T) {
	embyDir := t.TempDir()
	scanner := &Scanner{
		embyDir:   embyDir,
		publicURL: "http://example.com:8080",
		strmToken: "my-secret-token",
	}

	video := Video{
		File: pan.File{
			ID:   "video-file-123",
			Name: "IPX-123.mp4",
			Size: 2 << 30,
		},
		Code: "IPX-123",
	}

	scanner.writeFastSTRM(video)

	strmFile := filepath.Join(embyDir, "IPX", "IPX-123", "IPX-123.strm")
	data, err := os.ReadFile(strmFile)
	if err != nil {
		t.Fatalf("failed to read generated STRM file: %v", err)
	}

	content := strings.TrimSpace(string(data))
	expected := "http://example.com:8080/api/strm/play/video-file-123?token=my-secret-token"
	if content != expected {
		t.Fatalf("got %q, want %q", content, expected)
	}

	// Idempotent: existing file is not modified
	scanner.writeFastSTRM(video)
	data2, err := os.ReadFile(strmFile)
	if err != nil || string(data2) != string(data) {
		t.Fatalf("idempotent check failed: %v", err)
	}
}

func TestCheckpointSerializationAndRestoration(t *testing.T) {
	dirs := []Directory{
		{ID: "dir-2", Path: "/Movies/Dir2"},
		{ID: "dir-3", Path: "/Movies/Dir3"},
	}
	bytes, err := json.Marshal(dirs)
	if err != nil {
		t.Fatal(err)
	}

	payload := Payload{
		Checkpoint: string(bytes),
	}

	var restored []Directory
	if payload.Checkpoint != "" {
		if err := json.Unmarshal([]byte(payload.Checkpoint), &restored); err != nil {
			t.Fatal(err)
		}
	}

	if len(restored) != 2 || restored[0].ID != "dir-2" || restored[1].Path != "/Movies/Dir3" {
		t.Fatalf("unexpected restored directories: %+v", restored)
	}
}

func TestDefaultPacing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	// With cancelled context, DefaultPacing should return ctx.Err()
	err := DefaultPacing(ctx)
	if err == nil {
		t.Fatal("expected context error on timeout, got nil")
	}
}
