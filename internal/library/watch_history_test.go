package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/watchhistory"
)

func historyFilm(t *testing.T, lib *Service, source domain.LibrarySource, code string) (*ent.Movie, *ent.File) {
	t.Helper()
	film := lib.database.Movie.Create().SetCode(code).SetTitle("Title " + code).SetWatched(true).SaveX(t.Context())
	video := lib.database.File.Create().SetFileID(code).SetName(code + ".mp4").SetSize(1 << 30).
		SetAccountID(source.AccountID).SetRootID(source.Directory.ID).SetMovie(film).SaveX(t.Context())
	return film, video
}

func TestWatchHistoryPaginatesRecentMoviesWithBoundedScopedQueries(t *testing.T) {
	lib, _, payload := libraryFixture(t)
	ctx := t.Context()
	stamp := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	for i := range 23 {
		film, _ := historyFilm(t, lib, payload.Source, fmt.Sprintf("ABP-%03d", i))
		lib.database.WatchHistory.Create().SetAccountID(payload.Source.AccountID).SetRootID(payload.Source.Directory.ID).
			SetMovie(film).SetSessionID(uuid.NewString()).SetWatchedAt(stamp.Add(time.Duration(i) * time.Minute)).
			SetPosition(float64(i)).SetDuration(100).ExecX(ctx)
		// Multiple video files must not duplicate a movie in history or its count.
		lib.database.File.Create().SetFileID(fmt.Sprintf("part-%d", i)).SetName("part.mp4").SetSize(1 << 30).
			SetAccountID(payload.Source.AccountID).SetRootID(payload.Source.Directory.ID).SetMovie(film).ExecX(ctx)
	}
	for i, source := range []domain.LibrarySource{
		{AccountID: "other", Directory: payload.Source.Directory},
		{AccountID: payload.Source.AccountID, Directory: domain.LibraryDirectory{ID: "other"}},
	} {
		film, _ := historyFilm(t, lib, source, fmt.Sprintf("HIDDEN-%d", i))
		lib.database.WatchHistory.Create().SetAccountID(source.AccountID).SetRootID(source.Directory.ID).
			SetMovie(film).SetSessionID(uuid.NewString()).ExecX(ctx)
	}
	queries := map[string]int{}
	lib.database.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			queries[fmt.Sprintf("%T", query)]++
			return next.Query(ctx, query)
		})
	}))
	page, err := lib.WatchHistory(ctx, 1)
	if err != nil || page.Source == nil || *page.Source != payload.Source || page.Total != 23 || !page.HasMore || len(page.Items) != 20 {
		t.Fatalf("incorrect first page: %+v, %v", page, err)
	}
	if page.Items[0].Code != "ABP-022" || page.Items[0].Position != 22 || page.Items[0].Duration != 100 || page.Items[19].Code != "ABP-003" {
		t.Fatalf("history lost progress or recent order: %+v", page.Items)
	}
	want := map[string]int{"*ent.WatchHistoryQuery": 2, "*ent.MovieQuery": 1}
	if !reflect.DeepEqual(queries, want) {
		t.Fatalf("history loaded redundant data: %v", queries)
	}
	page, err = lib.WatchHistory(ctx, 2)
	if err != nil || len(page.Items) != 3 || page.HasMore || page.Items[2].Code != "ABP-000" {
		t.Fatalf("incorrect final page: %+v, %v", page, err)
	}
	page, err = lib.WatchHistory(ctx, 3)
	if err != nil || len(page.Items) != 0 || page.Total != 23 || page.Items == nil {
		t.Fatalf("out-of-range page lost its total or empty array: %+v, %v", page, err)
	}
	if err := lib.drive.ClearDirectory(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = lib.WatchHistory(ctx, 1)
	if err != nil || page.Source != nil || page.Total != 0 || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("unmounted history must be empty: %+v, %v", page, err)
	}
}

func TestWatchProgressPreservesResumeAndRejectsStaleSessionsAndVersions(t *testing.T) {
	lib, _, payload := libraryFixture(t)
	ctx := t.Context()
	film, video := historyFilm(t, lib, payload.Source, "ABP-001")
	session, err := lib.MarkWatched(ctx, film.ID, testWatchScope(payload.Source))
	if err != nil {
		t.Fatal(err)
	}
	before := lib.tasks.Revisions()
	progress := WatchProgress{SessionID: session.SessionID, FileID: video.FileID, Position: 120, Duration: 600, Version: 2}
	if err := lib.SaveWatchProgress(ctx, session.ID, progress); err != nil {
		t.Fatal(err)
	}
	after := lib.tasks.Revisions()
	if after.History != before.History+1 || after.Library != before.Library || after.Offline != before.Offline {
		t.Fatal("saving progress refreshed unrelated library or offline data")
	}
	progress.Version, progress.Position = 1, 300
	if err := lib.SaveWatchProgress(ctx, session.ID, progress); err != nil {
		t.Fatal(err)
	}
	if row := lib.database.WatchHistory.GetX(ctx, session.ID); row.Position != 120 || row.ProgressVersion != 2 || lib.tasks.Revisions() != after {
		t.Fatalf("an older request overwrote progress: %+v", row)
	}
	// Seeking backward is valid when it belongs to a newer request.
	progress.Version, progress.Position = 3, 30
	if err := lib.SaveWatchProgress(ctx, session.ID, progress); err != nil {
		t.Fatal(err)
	}
	reopened, err := lib.MarkWatched(ctx, film.ID, testWatchScope(payload.Source))
	if err != nil || reopened.ID != session.ID || reopened.SessionID == session.SessionID || reopened.Position != 30 || reopened.Duration != 600 || reopened.FileID != video.FileID {
		t.Fatalf("reopening lost resume information: %+v, %v", reopened, err)
	}
	progress.Version, progress.Position = 100, 550
	if err := lib.SaveWatchProgress(ctx, session.ID, progress); err != nil {
		t.Fatal(err)
	}
	if row := lib.database.WatchHistory.GetX(ctx, session.ID); row.Position != 30 || row.ProgressVersion != 0 {
		t.Fatal("a previous playback session replaced the new session")
	}
	progress.SessionID, progress.Version, progress.Position = reopened.SessionID, 1, 601
	if err := lib.SaveWatchProgress(ctx, session.ID, progress); err != nil {
		t.Fatal(err)
	}
	if row := lib.database.WatchHistory.GetX(ctx, session.ID); row.Position != 600 {
		t.Fatal("completed playback escaped its duration")
	}
}

func TestWatchProgressValidatesFilesNumbersAndMountedSource(t *testing.T) {
	lib, _, payload := libraryFixture(t)
	ctx := t.Context()
	film, video := historyFilm(t, lib, payload.Source, "ABP-001")
	_, otherVideo := historyFilm(t, lib, payload.Source, "ABP-002")
	session, err := lib.MarkWatched(ctx, film.ID, testWatchScope(payload.Source))
	if err != nil {
		t.Fatal(err)
	}
	progress := WatchProgress{SessionID: session.SessionID, FileID: video.FileID, Position: 1, Duration: 600, Version: 1}
	for _, mutate := range []func(*WatchProgress){
		func(p *WatchProgress) { p.Position = -1 }, func(p *WatchProgress) { p.Position = math.NaN() },
		func(p *WatchProgress) { p.Duration = math.Inf(1) }, func(p *WatchProgress) { p.Duration = 0 },
		func(p *WatchProgress) { p.Version = 0 },
	} {
		invalid := progress
		mutate(&invalid)
		if err := lib.SaveWatchProgress(ctx, session.ID, invalid); !errors.Is(err, ErrInvalidWatchProgress) {
			t.Fatalf("invalid progress was accepted: %+v, %v", invalid, err)
		}
	}
	progress.FileID = otherVideo.FileID
	if err := lib.SaveWatchProgress(ctx, session.ID, progress); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a file from another movie was accepted: %v", err)
	}
	progress.FileID = video.FileID
	mountSource(t, lib.drive, stubOf(t, lib.drive), domain.LibrarySource{
		AccountID: "other", Directory: payload.Source.Directory,
	})
	if err := lib.SaveWatchProgress(ctx, session.ID, progress); !ent.IsNotFound(err) {
		t.Fatalf("an inactive source accepted progress: %v", err)
	}
	if row := lib.database.WatchHistory.GetX(ctx, session.ID); row.Position != 0 || row.ProgressVersion != 0 {
		t.Fatal("rejected progress mutated the stored record")
	}
}

func TestClearingHistoryPreservesMoviesAndCannotBeUndoneByLateProgress(t *testing.T) {
	lib, _, payload := libraryFixture(t)
	ctx := t.Context()
	first, video := historyFilm(t, lib, payload.Source, "ABP-001")
	second, _ := historyFilm(t, lib, payload.Source, "ABP-002")
	firstSession, err := lib.MarkWatched(ctx, first.ID, testWatchScope(payload.Source))
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := lib.MarkWatched(ctx, second.ID, testWatchScope(payload.Source))
	if err != nil {
		t.Fatal(err)
	}
	other := lib.database.WatchHistory.Create().SetAccountID("other").SetRootID("10").SetMovie(first).
		SetSessionID(uuid.NewString()).SaveX(ctx)
	scope := domain.WatchHistoryScope{AccountID: payload.Source.AccountID, DirectoryID: payload.Source.Directory.ID}
	if count, err := lib.RemoveWatchHistory(ctx, scope, nil); err != nil || count != 0 {
		t.Fatalf("empty selection must not clear history: %d, %v", count, err)
	}
	if _, err := lib.ClearWatchHistory(ctx, domain.WatchHistoryScope{AccountID: "other", DirectoryID: "10"}); !errors.Is(err, ErrWatchHistorySourceChanged) {
		t.Fatalf("a stale clear operation was accepted: %v", err)
	}
	if count, err := lib.RemoveWatchHistory(ctx, scope, []int{firstSession.ID, other.ID}); err != nil || count != 1 {
		t.Fatalf("selected deletion escaped its source: %d, %v", count, err)
	}
	progress := WatchProgress{SessionID: firstSession.SessionID, FileID: video.FileID, Position: 100, Duration: 600, Version: 1}
	if err := lib.SaveWatchProgress(ctx, firstSession.ID, progress); !ent.IsNotFound(err) {
		t.Fatalf("late progress recreated a cleared record: %v", err)
	}
	if count, err := lib.ClearWatchHistory(ctx, scope); err != nil || count != 1 {
		t.Fatalf("clear all did not remove the remaining record: %d, %v", count, err)
	}
	if lib.database.WatchHistory.Query().CountX(ctx) != 1 || !lib.database.WatchHistory.Query().Where(watchhistory.IDEQ(other.ID)).ExistX(ctx) {
		t.Fatal("clear all removed another account's history")
	}
	if !lib.database.Movie.GetX(ctx, first.ID).Watched || lib.database.File.Query().CountX(ctx) != 2 {
		t.Fatal("clearing history removed files or reset watched badges")
	}
	if lib.database.WatchHistory.Query().Where(watchhistory.IDEQ(secondSession.ID)).ExistX(ctx) {
		t.Fatal("clear all left an active history record")
	}
	// Removing an orphan movie during scanning must cascade to history.
	lib.database.File.Delete().Where(file.MovieIDEQ(first.ID)).ExecX(ctx)
	lib.database.Movie.DeleteOneID(first.ID).ExecX(ctx)
	if lib.database.WatchHistory.Query().CountX(ctx) != 0 {
		t.Fatal("removed movie left orphan history")
	}
}
