package library

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	drivePkg "github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scan"
	scrapePkg "github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

func TestScanDiscardsLatePageAfterSourceChange(t *testing.T) {
	lib, client := panConcurrencyFixture(t)
	drive, ctx := lib.drive, t.Context()
	source := *drive.Source()
	queued := lib.database.Task.Query().Where(task.TypeEQ("scan")).OnlyX(ctx)
	payload := scan.Payload{Source: source, Scan: domain.ScanProgress{Stage: "scanning"}}
	if err := indexScanPage(ctx, lib, queued.ID, "previous-scan", "/Movies", []scan.Video{fixtureVideo("101", "ABP-001.mp4")}, &payload); err != nil {
		t.Fatal(err)
	}
	queued = lib.database.Task.GetX(ctx, queued.ID)
	started := make(chan struct{}, 1)
	hold, release := panTestGate(t)
	client.list = func(context.Context, string, string, int, int) (pan.FilePage, error) {
		started <- struct{}{}
		<-hold
		return pan.FilePage{Total: 1, Files: []pan.File{fixtureVideo("late", "ABP-002.mp4").File},
			Path: []pan.Directory{{ID: source.Directory.ID, Name: source.Directory.Name}}}, nil
	}
	finished := make(chan error, 1)
	go func() { finished <- lib.Scan(ctx, tasks.Job{ID: queued.ID, Type: "scan", Payload: queued.Payload}) }()
	awaitPan(t, started)
	if err := drive.ClearDirectory(ctx); err != nil {
		t.Fatal(err)
	}
	release()
	if err := awaitPan(t, finished); !errors.Is(err, drivePkg.ErrSourceChanged) {
		t.Fatalf("stale scan = %v", err)
	}
	files := lib.database.File.Query().AllX(ctx)
	if len(files) != 1 || files[0].FileID != "101" || files[0].ScanID != "previous-scan" {
		t.Fatalf("stale page indexed or pruned files: %+v", files)
	}
	if lib.database.Task.Query().Where(task.TypeEQ("scrape")).ExistX(ctx) {
		t.Fatal("stale scan enqueued metadata work")
	}
}

func TestMetadataSourceChangeAfterInfoPreventsUpload(t *testing.T) {
	lib, client := panConcurrencyFixture(t)
	source := *lib.drive.Source()
	started := make(chan struct{}, 1)
	hold, release := panTestGate(t)
	video := pan.File{ID: "video", ParentID: source.Directory.ID, Name: "ABP-001.mp4"}
	client.info = func(context.Context, string, string) (pan.FileInfo, error) {
		started <- struct{}{}
		<-hold
		return pan.FileInfo{File: video, Path: []pan.Directory{{ID: source.Directory.ID}}}, nil
	}
	var uploads atomic.Int32
	client.uploadMetadata = func(context.Context, string, string, string, []byte) error {
		uploads.Add(1)
		return nil
	}
	finished := make(chan error, 1)
	go func() {
		sess, err := lib.drive.OpenSource(t.Context(), source)
		if err != nil {
			finished <- err
			return
		}
		finished <- scrapePkg.UploadSidecar(t.Context(), sess, scrapePkg.MovieDirectory{
			ID: source.Directory.ID, Files: []pan.File{video}, VideoIDs: map[string]bool{video.ID: true},
		}, "movie.nfo", []byte("fixture"))
	}()
	awaitPan(t, started)
	if err := lib.drive.ClearDirectory(t.Context()); err != nil {
		t.Fatal(err)
	}
	release()
	if err := awaitPan(t, finished); !errors.Is(err, drivePkg.ErrSourceChanged) {
		t.Fatalf("stale metadata upload = %v", err)
	}
	if uploads.Load() != 0 {
		t.Fatal("metadata was uploaded after the directory changed")
	}
}
