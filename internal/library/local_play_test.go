package library

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
)

func localPlayFixture(t *testing.T) (*Service, string) {
	t.Helper()
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(store.Client, nil, nil, nil, Options{}), t.TempDir()
}

func TestLocalVideosAndPlayFile(t *testing.T) {
	ctx := t.Context()
	lib, root := localPlayFixture(t)
	body := []byte("fake-mp4-bytes")
	if err := os.WriteFile(filepath.Join(root, "PFES-088.mp4"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	record := lib.database.Movie.Create().SetCode("PFES-088").SaveX(ctx)
	lib.database.File.Create().SetFileID("local-1").SetName("PFES-088.mp4").SetSize(int64(len(body))).
		SetAccountID(domain.LocalAccountID).SetRootID(root).SetPath("PFES-088.mp4").SetMovieID(record.ID).SaveX(ctx)
	lib.database.File.Create().SetFileID("remote-1").SetName("PFES-088.strm").SetSize(10).
		SetAccountID(domain.LocalAccountID).SetRootID(root).SetPath("PFES-088.strm").SetMovieID(record.ID).SaveX(ctx)

	videos, err := lib.LocalVideos(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 || videos[0].FileID != "local-1" || videos[0].Name != "PFES-088.mp4" || videos[0].Size != int64(len(body)) {
		t.Fatalf("videos=%+v", videos)
	}

	opened, name, err := lib.LocalPlayFile(ctx, "local-1")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	got, err := io.ReadAll(opened)
	if err != nil {
		t.Fatal(err)
	}
	if name != "PFES-088.mp4" || !bytes.Equal(got, body) {
		t.Fatalf("opened %q with %q", name, got)
	}

	if _, _, err := lib.LocalPlayFile(ctx, "remote-1"); !domain.IsKind(err, domain.KindNotFound) {
		t.Fatalf("strm play err = %v", err)
	}
	if _, _, err := lib.LocalPlayFile(ctx, "missing"); !domain.IsKind(err, domain.KindNotFound) {
		t.Fatalf("missing play err = %v", err)
	}
}

func TestMovieDetailIncludesLocalVideos(t *testing.T) {
	ctx := t.Context()
	lib, root := localPlayFixture(t)
	if err := os.WriteFile(filepath.Join(root, "PFES-088.mp4"), []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := lib.database.Movie.Create().SetCode("PFES-088").SaveX(ctx)
	lib.database.File.Create().SetFileID("local-1").SetName("PFES-088.mp4").SetSize(1).
		SetAccountID(domain.LocalAccountID).SetRootID(root).SetPath("PFES-088.mp4").SetMovieID(record.ID).SaveX(ctx)

	detail, err := lib.Movie(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Videos) != 1 || detail.Videos[0].FileID != "local-1" {
		t.Fatalf("detail videos=%+v", detail.Videos)
	}
}

func TestLocalPlayFileRejectsEscapingPath(t *testing.T) {
	ctx := t.Context()
	lib, parent := localPlayFixture(t)
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "escape.mp4"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := lib.database.Movie.Create().SetCode("IPX-1").SaveX(ctx)
	lib.database.File.Create().SetFileID("local-escape").SetName("escape.mp4").SetSize(6).
		SetAccountID(domain.LocalAccountID).SetRootID(root).SetPath("../escape.mp4").SetMovieID(record.ID).SaveX(ctx)

	if _, _, err := lib.LocalPlayFile(ctx, "local-escape"); err == nil {
		t.Fatal("escaping path was served")
	}
}
