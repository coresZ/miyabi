package scan

import (
	"fmt"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/tasks"
)

func localScanStore(t *testing.T) *database.Store {
	t.Helper()
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestEnqueueLocalScrapesQueuesPendingLocalMovies(t *testing.T) {
	store := localScanStore(t)
	ctx := t.Context()
	source := domain.LibrarySource{AccountID: domain.LocalAccountID,
		Directory: domain.LibraryDirectory{ID: "src-1", Name: "本地", Path: `D:\media`}}
	record := store.Client.Movie.Create().SetCode("IPX-123").SaveX(ctx)
	store.Client.File.Create().SetFileID("local-1").SetName("IPX-123.mp4").SetSize(1 << 30).
		SetAccountID(domain.LocalAccountID).SetRootID(domain.LocalAccountID).SetPath("IPX-123.mp4").SetMovieID(record.ID).SaveX(ctx)

	queued, err := EnqueueLocalScrapes(ctx, store.Client, 7, source)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued=%d, want 1", queued)
	}
	job := store.Client.Task.Query().Where(task.TypeEQ(string(tasks.KindScrape))).OnlyX(ctx)
	if job.ResourceKey != fmt.Sprintf("movie:%d", record.ID) {
		t.Fatalf("resource_key=%q", job.ResourceKey)
	}
	payload, err := tasks.DecodePayload[scrape.MetadataPayload](job.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if payload.MovieID != record.ID || payload.Code != "IPX-123" || payload.ScanTaskID != 7 || payload.Source != source {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestEnqueueLocalScrapesSkipsDoneActiveAndRemote(t *testing.T) {
	store := localScanStore(t)
	ctx := t.Context()
	source := domain.LibrarySource{AccountID: domain.LocalAccountID,
		Directory: domain.LibraryDirectory{ID: "src-1", Name: "本地", Path: `D:\media`}}

	done := store.Client.Movie.Create().SetCode("DONE-1").SetScrapeStatus(movie.ScrapeStatusDone).SaveX(ctx)
	store.Client.File.Create().SetFileID("local-done").SetName("DONE-1.mp4").SetSize(1).
		SetAccountID(domain.LocalAccountID).SetRootID(domain.LocalAccountID).SetPath("DONE-1.mp4").SetMovieID(done.ID).SaveX(ctx)

	active := store.Client.Movie.Create().SetCode("ACTIVE-1").SaveX(ctx)
	store.Client.File.Create().SetFileID("local-active").SetName("ACTIVE-1.mp4").SetSize(1).
		SetAccountID(domain.LocalAccountID).SetRootID(domain.LocalAccountID).SetPath("ACTIVE-1.mp4").SetMovieID(active.ID).SaveX(ctx)
	store.Client.Task.Create().SetType(tasks.KindScrape.String()).
		SetResourceKey(fmt.Sprintf("movie:%d", active.ID)).SetStatus(task.StatusRunning).SaveX(ctx)

	remote := store.Client.Movie.Create().SetCode("REMOTE-1").SaveX(ctx)
	store.Client.File.Create().SetFileID("remote-1").SetName("REMOTE-1.mp4").SetSize(1).
		SetAccountID("100").SetRootID("10").SetPath("REMOTE-1.mp4").SetMovieID(remote.ID).SaveX(ctx)

	want := store.Client.Movie.Create().SetCode("NEW-1").SaveX(ctx)
	store.Client.File.Create().SetFileID("local-new").SetName("NEW-1.mp4").SetSize(1).
		SetAccountID(domain.LocalAccountID).SetRootID(domain.LocalAccountID).SetPath("NEW-1.mp4").SetMovieID(want.ID).SaveX(ctx)

	queued, err := EnqueueLocalScrapes(ctx, store.Client, 9, source)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued=%d, want 1", queued)
	}
	jobs := store.Client.Task.Query().Where(task.TypeEQ(string(tasks.KindScrape))).AllX(ctx)
	if len(jobs) != 2 {
		t.Fatalf("scrape tasks=%d, want 1 existing active + 1 queued", len(jobs))
	}
	created := store.Client.Task.Query().Where(task.TypeEQ(string(tasks.KindScrape)),
		task.ResourceKeyEQ(fmt.Sprintf("movie:%d", want.ID))).OnlyX(ctx)
	if created.Status != task.StatusQueued {
		t.Fatalf("queued task status=%q", created.Status)
	}
}
