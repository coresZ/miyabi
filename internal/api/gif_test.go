package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func minimalGIF() []byte {
	return []byte{
		0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00,
		0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02,
		0x44, 0x01, 0x00, 0x3b,
	}
}

func TestWriteGifFileDedup(t *testing.T) {
	dir := t.TempDir()
	raw := minimalGIF()
	sum := sha256.Sum256(raw)
	prefix := hex.EncodeToString(sum[:])[:gifHashPrefix]

	first, err := writeGifFile(dir, "Demo Title.mp4", raw)
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate {
		t.Fatal("first save should not be duplicate")
	}
	if !strings.HasPrefix(first.Filename, prefix+"_") {
		t.Fatalf("filename %q missing hash prefix", first.Filename)
	}
	path := filepath.Join(dir, gifDirName, first.Filename)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	second, err := writeGifFile(dir, "other-name", raw)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate {
		t.Fatal("expected duplicate")
	}
	if second.Filename != first.Filename {
		t.Fatalf("duplicate should reuse name: %q vs %q", second.Filename, first.Filename)
	}

	entries, _ := os.ReadDir(filepath.Join(dir, gifDirName))
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
}

func TestSaveGifUploadMultipart(t *testing.T) {
	dir := t.TempDir()
	raw := minimalGIF()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile(gifFormFileKey, "clip.gif")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(raw); err != nil {
		t.Fatal(err)
	}
	_ = w.WriteField(gifFormNameKey, "My Clip")
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/player/gif", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	result, err := saveGifUpload(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Duplicate || result.Size != int64(len(raw)) {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestBuildGifFilename(t *testing.T) {
	name := buildGifFilename("abcd", "../../evil name?.gif")
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		t.Fatalf("unsafe name: %s", name)
	}
	if !strings.HasSuffix(name, ".gif") {
		t.Fatalf("missing extension: %s", name)
	}
}
