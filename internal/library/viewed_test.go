package library

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/ent/setting"
)

func TestViewedMovies_CRUDAndOrdering(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	svc := New(store.Client, nil, nil, nil)

	// Initially empty
	ids, err := svc.ViewedMovieIDs(ctx)
	if err != nil {
		t.Fatalf("ViewedMovieIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty ids, got %v", ids)
	}

	// Empty / whitespace ignored
	if err := svc.AddViewedMovieIDs(ctx, []string{"", "   "}); err != nil {
		t.Fatalf("AddViewedMovieIDs() empty error = %v", err)
	}
	ids, err = svc.ViewedMovieIDs(ctx)
	if err != nil || len(ids) != 0 {
		t.Fatalf("expected empty ids after blank add, got %v", ids)
	}

	// Add batch
	if err := svc.AddViewedMovieIDs(ctx, []string{"id-1", " id-2 ", "id-1", "id-3"}); err != nil {
		t.Fatalf("AddViewedMovieIDs() error = %v", err)
	}
	ids, err = svc.ViewedMovieIDs(ctx)
	if err != nil {
		t.Fatalf("ViewedMovieIDs() error = %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 ids, got %v", ids)
	}

	// Re-add id-1 should move it to the front
	if err := svc.AddViewedMovieIDs(ctx, []string{"id-1"}); err != nil {
		t.Fatalf("AddViewedMovieIDs() re-add error = %v", err)
	}
	ids, err = svc.ViewedMovieIDs(ctx)
	if err != nil {
		t.Fatalf("ViewedMovieIDs() error = %v", err)
	}
	if len(ids) != 3 || ids[0] != "id-1" {
		t.Fatalf("expected id-1 at front, got %v", ids)
	}
}

func TestViewedMovies_MigrationFromSettings(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Insert legacy setting
	store.Client.Setting.Create().
		SetKey(viewedMoviesLegacySetting).
		SetValue(jsontext.Value(`{"ids":["legacy-1","legacy-2","legacy-3"]}`)).
		SaveX(ctx)

	// Instantiating library should run migration
	svc := New(store.Client, nil, nil, nil)

	ids, err := svc.ViewedMovieIDs(ctx)
	if err != nil {
		t.Fatalf("ViewedMovieIDs() error = %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 migrated ids, got %v", ids)
	}

	// Legacy setting record must be deleted
	exists, err := store.Client.Setting.Query().Where(setting.KeyEQ(viewedMoviesLegacySetting)).Exist(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("legacy setting was not cleaned up after migration")
	}

	// Subsequence New() calls should be idempotent
	svc2 := New(store.Client, nil, nil, nil)
	count, err := store.Client.ViewedMovie.Query().Count(ctx)
	if err != nil || count != 3 {
		t.Fatalf("idempotent migration changed count: %d, %v", count, err)
	}
	_ = svc2
}
