package library

import (
	"errors"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/library/scan"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/nfo"
)

func TestMarkWatchedIsLocalAndKeepsMovieStateIdempotent(t *testing.T) {
	lib, queued, payload := libraryFixture(t)
	ctx := t.Context()
	if err := indexScanPage(ctx, lib, queued.ID, "first", "/Movies",
		[]scan.Video{fixtureVideo("video", "ABP-001.mp4")}, &payload); err != nil {
		t.Fatal(err)
	}
	film := lib.database.Movie.Query().OnlyX(ctx)
	if film.Watched {
		t.Fatal("new movies must start unwatched")
	}
	before := lib.tasks.Revisions()
	// The fixture has no 115 client; recording an open must work locally.
	session, err := lib.MarkWatched(ctx, film.ID, testWatchScope(payload.Source))
	if err != nil {
		t.Fatal(err)
	}
	updated := lib.database.Movie.GetX(ctx, film.ID)
	after := lib.tasks.Revisions()
	if !updated.Watched || after.Library != before.Library+1 || after.Offline != before.Offline {
		t.Fatalf("watch change was not persisted and published once: movie=%+v before=%+v after=%+v", updated, before, after)
	}
	page, err := lib.Movies(ctx, 1, 24)
	if err != nil || len(page.Movies) != 1 || !page.Movies[0].Watched {
		t.Fatalf("library did not return the saved watch state: %+v, %v", page, err)
	}
	nextSession, err := lib.MarkWatched(ctx, film.ID, testWatchScope(payload.Source))
	if err != nil {
		t.Fatal(err)
	}
	repeated := lib.database.Movie.GetX(ctx, film.ID)
	if !repeated.UpdatedAt.Equal(updated.UpdatedAt) || lib.tasks.Revisions().Library != after.Library ||
		lib.tasks.Revisions().History != after.History+1 {
		t.Fatal("reopening must update history without rewriting movie metadata")
	}
	if nextSession.ID != session.ID || nextSession.SessionID == session.SessionID || lib.database.WatchHistory.Query().CountX(ctx) != 1 {
		t.Fatal("reopening must retain one history entry and start a new progress session")
	}
}

func TestMarkWatchedRequiresAMovieInTheMountedSource(t *testing.T) {
	lib, _, payload := libraryFixture(t)
	ctx := t.Context()
	for _, scenario := range []struct{ code, account, root string }{
		{"ABP-001", "other-account", payload.Source.Directory.ID},
		{"ABP-002", payload.Source.AccountID, "other-root"},
		{"ABP-003", "", ""},
	} {
		film := lib.database.Movie.Create().SetCode(scenario.code).SaveX(ctx)
		if scenario.account != "" {
			lib.database.File.Create().SetFileID(scenario.code).SetName(scenario.code + ".mp4").SetSize(1 << 30).
				SetAccountID(scenario.account).SetRootID(scenario.root).SetMovie(film).ExecX(ctx)
		}
		before := lib.tasks.Revisions()
		if _, err := lib.MarkWatched(ctx, film.ID, testWatchScope(payload.Source)); !ent.IsNotFound(err) {
			t.Fatalf("movie %s outside the mounted source was accepted: %v", film.Code, err)
		}
		if lib.database.Movie.GetX(ctx, film.ID).Watched || lib.tasks.Revisions() != before {
			t.Fatal("rejected watch request changed movie state")
		}
	}
	if _, err := lib.MarkWatched(ctx, 99999, testWatchScope(payload.Source)); !ent.IsNotFound(err) {
		t.Fatalf("missing movie was accepted: %v", err)
	}
	if err := lib.drive.ClearDirectory(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.MarkWatched(ctx, 1, testWatchScope(payload.Source)); !errors.Is(err, drive.ErrMediaDirectoryRequired) {
		t.Fatalf("unmounted library was accepted: %v", err)
	}
}

func TestMarkWatchedRejectsTheSourceOfAnOlderOpening(t *testing.T) {
	lib, queued, payload := libraryFixture(t)
	ctx := t.Context()
	if err := indexScanPage(ctx, lib, queued.ID, "first", "/Movies",
		[]scan.Video{fixtureVideo("video", "ABP-001.mp4")}, &payload); err != nil {
		t.Fatal(err)
	}
	film := lib.database.Movie.Query().OnlyX(ctx)
	before := lib.tasks.Revisions()
	for _, scope := range []domain.WatchHistoryScope{
		{AccountID: "old-account", DirectoryID: payload.Source.Directory.ID},
		{AccountID: payload.Source.AccountID, DirectoryID: "old-directory"},
	} {
		if _, err := lib.MarkWatched(ctx, film.ID, scope); !errors.Is(err, ErrWatchHistorySourceChanged) {
			t.Fatalf("stale opening source accepted: %v", err)
		}
	}
	if lib.database.Movie.GetX(ctx, film.ID).Watched || lib.database.WatchHistory.Query().CountX(ctx) != 0 ||
		lib.tasks.Revisions() != before {
		t.Fatal("a stale opening wrote watch state into the current source")
	}
}

func TestWatchedStateSurvivesScrapingDownloadIndexingAndRescan(t *testing.T) {
	lib, queued, payload := libraryFixture(t)
	ctx := t.Context()
	videos := []scan.Video{fixtureVideo("video", "ABP-001.mp4")}
	if err := indexScanPage(ctx, lib, queued.ID, "first", "/Movies", videos, &payload); err != nil {
		t.Fatal(err)
	}
	film := lib.database.Movie.Query().OnlyX(ctx)
	if _, err := lib.MarkWatched(ctx, film.ID, testWatchScope(payload.Source)); err != nil {
		t.Fatal(err)
	}
	doc := nfo.Movie{Code: film.Code, Title: "Updated title",
		IDs: []nfo.UniqueID{{Type: "javdb", Default: true, Value: "catalogue-id"}}}
	if err := ent.WithTx(ctx, lib.database, func(tx *ent.Tx) error {
		if err := scrape.SaveMovieMetadata(ctx, tx, film.ID, doc); err != nil {
			return err
		}
		_, err := scan.IndexDownloadedMovie(ctx, tx, scan.Payload{Code: film.Code, JavDBID: doc.JavDBID()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := indexScanPage(ctx, lib, queued.ID, "rescan", "/Movies", videos, &payload); err != nil {
		t.Fatal(err)
	}
	got := lib.database.Movie.GetX(ctx, film.ID)
	if !got.Watched || got.Title != doc.Title || got.ScrapeStatus != movie.ScrapeStatusPending {
		t.Fatalf("metadata/index updates lost watch state: %+v", got)
	}
}
