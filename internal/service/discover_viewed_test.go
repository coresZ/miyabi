package service

import (
	"slices"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/javdb"
)

func TestDiscoverViewedMovieIDs(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc, err := NewDiscoverService(ctx, store.Client, javdb.Options{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	// Initial query should return empty slice
	ids, err := svc.ViewedMovieIDs(ctx)
	if err != nil {
		t.Fatalf("ViewedMovieIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("initial IDs len = %d, want 0", len(ids))
	}

	// Add empty / invalid IDs
	if err := svc.AddViewedMovieIDs(ctx, []string{"", "   "}); err != nil {
		t.Fatalf("AddViewedMovieIDs() empty error = %v", err)
	}
	ids, err = svc.ViewedMovieIDs(ctx)
	if err != nil || len(ids) != 0 {
		t.Fatalf("after empty add: ids = %v, err = %v", ids, err)
	}

	// Add batch with duplicates and whitespace
	if err := svc.AddViewedMovieIDs(ctx, []string{"id-1", " id-2 ", "id-1", "id-3"}); err != nil {
		t.Fatalf("AddViewedMovieIDs() error = %v", err)
	}
	ids, err = svc.ViewedMovieIDs(ctx)
	if err != nil {
		t.Fatalf("ViewedMovieIDs() error = %v", err)
	}
	want := []string{"id-1", "id-2", "id-3"}
	if !slices.Equal(ids, want) {
		t.Fatalf("got %v, want %v", ids, want)
	}

	// Add another batch: new IDs become most recent
	if err := svc.AddViewedMovieIDs(ctx, []string{"id-4", "id-2"}); err != nil {
		t.Fatalf("AddViewedMovieIDs() second batch error = %v", err)
	}
	ids, err = svc.ViewedMovieIDs(ctx)
	if err != nil {
		t.Fatalf("ViewedMovieIDs() error = %v", err)
	}
	want = []string{"id-4", "id-2", "id-1", "id-3"}
	if !slices.Equal(ids, want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
}
