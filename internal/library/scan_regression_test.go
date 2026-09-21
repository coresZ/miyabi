package library

import (
	"context"
	"testing"

	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scan"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

func TestScanProgressKeepsRestartContextAndScanKind(t *testing.T) {
	for _, targeted := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "targeted"}[targeted], func(t *testing.T) {
			lib, queued, payload := libraryFixture(t)
			payload.Scan.FilesScanned = 100
			if targeted {
				payload.TargetID, payload.TargetPath, payload.TargetFile = "video", "/Movies/video.mp4", true
				payload.OfflineTaskID, payload.Code, payload.JavDBID = 123, "ABP-001", "catalogue-id"
			}
			if err := scan.SaveScanProgress(t.Context(), lib.database.Task, queued.ID, payload); err != nil {
				t.Fatal(err)
			}
			record := lib.database.Task.GetX(t.Context(), queued.ID)
			restored, err := tasks.DecodePayload[scan.Payload](record.Payload)
			if err != nil || restored != payload {
				t.Fatalf("restart context changed: %+v err=%v", restored, err)
			}
			next, err := lib.tasks.EnqueueScan(t.Context(), payload.Source)
			if err != nil || (next.ID == queued.ID) == targeted {
				t.Fatalf("full-scan deduplication confused targeted=%t: %+v err=%v", targeted, next, err)
			}
		})
	}
}

func TestMixedScanPageRollsBackBothChangedFilesAndUnchangedMarkers(t *testing.T) {
	lib, queued, payload := libraryFixture(t)
	videos := []scan.Video{fixtureVideo("101", "ABP-001.mp4"), fixtureVideo("102", "ABP-002.mp4")}
	if err := indexScanPage(t.Context(), lib, queued.ID, "previous", "/Movies", videos, &payload); err != nil {
		t.Fatal(err)
	}
	lib.database.Task.DeleteOneID(queued.ID).ExecX(t.Context())
	videos[1] = fixtureVideo("102", "ABP-003.mp4")
	if err := scan.ProcessScanPage(t.Context(), lib.database, queued.ID, "failed", "/Movies", videos, &payload,
		func(videos []scan.Video) []scan.Video { return videos }, lib.tasks); err == nil {
		t.Fatal("scan page without a progress record unexpectedly committed")
	}
	for _, id := range []string{"101", "102"} {
		if got := lib.database.File.Query().Where(file.FileIDEQ(id)).OnlyX(t.Context()); got.ScanID != "previous" {
			t.Fatalf("file %s escaped rollback: %+v", id, got)
		}
	}
	if got := lib.database.File.Query().Where(file.FileIDEQ("102")).OnlyX(t.Context()); got.Name != "ABP-002.mp4" {
		t.Fatal("renamed file escaped rollback")
	}
	if lib.database.Movie.Query().CountX(t.Context()) != 2 {
		t.Fatal("new movie escaped rollback")
	}
}

func TestScanResolvesAndIndexesCurrentIdentityWithOneFileRead(t *testing.T) {
	lib, queued, payload := libraryFixture(t)
	ctx := t.Context()
	film := lib.database.Movie.Create().SetCode("OLD-001").SetJavdbID("catalogue-id").SaveX(ctx)
	lib.database.File.Create().SetFileID("video").SetName("video.mp4").SetParentID("10").SetPath("/Movies/video.mp4").
		SetSize(1 << 30).SetSha1("original").SetAccountID(payload.Source.AccountID).SetRootID(payload.Source.Directory.ID).SetMovie(film).ExecX(ctx)
	// An already prepared filename/code must not override fresher catalogue data.
	videos := []scan.Video{{File: pan.File{ID: "video", ParentID: "10", Name: "video.mp4", Size: 1 << 30, SHA1: "original"}, Code: "STALE-001"}}
	film.Update().SetCode("CURRENT-001").ExecX(ctx)
	fileReads, movieReads := 0, 0
	lib.database.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			switch query.(type) {
			case *ent.FileQuery:
				fileReads++
			case *ent.MovieQuery:
				movieReads++
			}
			return next.Query(ctx, query)
		})
	}))
	if err := scan.ProcessScanPage(ctx, lib.database, queued.ID, "current", "/Movies", videos, &payload,
		func(videos []scan.Video) []scan.Video { return videos }, lib.tasks); err != nil {
		t.Fatal(err)
	}
	if fileReads != 1 || movieReads != 1 || videos[0].Code != "CURRENT-001" {
		t.Fatalf("scan reread or used stale identities: files=%d movies=%d videos=%#v", fileReads, movieReads, videos)
	}
	indexed := lib.database.File.Query().Where(file.FileIDEQ("video")).OnlyX(ctx)
	if indexed.MovieID == nil || *indexed.MovieID != film.ID || indexed.ScanID != "current" {
		t.Fatalf("scan changed a verified association: %#v", indexed)
	}
}

func TestCompactScanKeepsSharedDirectoryAndArtworkMatching(t *testing.T) {
	for _, scenario := range []struct {
		name, nfo, poster string
		shared            bool
		jobs              int
	}{
		{name: "generic NFO", nfo: "movie.nfo", poster: "cover.asset"},
		{name: "generic NFO with another video", nfo: "movie.nfo", poster: "cover.asset", shared: true, jobs: 1},
		{name: "exact NFO with another video", nfo: "ABP-001.nfo", poster: "cover.asset", shared: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newCompletedScanFixture(t)
			f.entries["10"][1].Name, f.entries["10"][2].Name = scenario.nfo, scenario.poster
			f.input.Snapshot.Directories[0].NFO.Name = scenario.nfo
			f.input.Snapshot.Directories[0].Poster.Name = scenario.poster
			// A directory ending in .nfo must not become a candidate sidecar.
			f.entries["10"] = append(f.entries["10"], pan.File{ID: "folder", Name: "other.nfo", IsDirectory: true})
			if scenario.shared {
				f.entries["10"] = append(f.entries["10"], pan.File{ID: "unmatched", Name: "recording.mp4", Size: 1 << 30})
			}
			encoded, err := tasks.EncodePayload(f.input)
			if err != nil {
				t.Fatal(err)
			}
			f.covered.Update().SetPayload(encoded).ExecX(t.Context())
			observed := make(scrape.DirectoryObservations)
			for _, entry := range f.entries["10"] {
				observed.Add("10", []pan.File{entry})
			}
			if err := indexScanPage(t.Context(), f.lib, f.queued.ID, "rescan", "/Movies", f.videos, &f.payload); err != nil {
				t.Fatal(err)
			}
			if err := reconcileScan(t.Context(), f.lib, f.queued.ID, "rescan", &f.payload, observed); err != nil {
				t.Fatal(err)
			}
			if count := f.lib.database.Task.Query().Where(task.TypeEQ("scrape")).CountX(t.Context()); count != scenario.jobs {
				t.Fatalf("metadata tasks=%d want=%d", count, scenario.jobs)
			}
		})
	}
}
