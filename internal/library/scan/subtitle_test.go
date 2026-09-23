package scan

import (
	"context"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestIndexDirectorySubtitles_MismatchedCodeNotFallBack(t *testing.T) {
	ctx := context.Background()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create movie ABP-123
	movieABP, err := store.Client.Movie.Create().SetCode("ABP-123").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := store.Client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	videos := []Video{
		{Code: "ABP-123"},
	}

	subtitles := []pan.File{
		// 1. Alien subtitle with explicit mismatched code -> MUST NOT be indexed
		{ID: "sub-alien", PickCode: "pick-alien", Name: "SSIS-001.chs.srt"},
		// 2. Generic language name without code in exclusive directory -> SHOULD be indexed
		{ID: "sub-generic", PickCode: "pick-generic", Name: "chinese.srt"},
		// 3. Matching code subtitle -> SHOULD be indexed
		{ID: "sub-matched", PickCode: "pick-matched", Name: "ABP-123.uncensored.cht.srt"},
	}

	if err := IndexDirectorySubtitles(ctx, tx, videos, subtitles); err != nil {
		t.Fatalf("IndexDirectorySubtitles failed: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Verify DB state
	records, err := store.Client.Subtitle.Query().Where(subtitle.MovieIDEQ(movieABP.ID)).All(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 subtitles indexed, got %d", len(records))
	}

	for _, r := range records {
		if r.FileID == "sub-alien" {
			t.Errorf("alien subtitle SSIS-001.chs.srt was incorrectly indexed to ABP-123")
		}
	}
}
