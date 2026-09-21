package service

import (
	"errors"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/tasks"
	"testing"

	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scan"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestMovieStatesFollowDownloadThroughIndexingWithoutCatalogueRequests(t *testing.T) {
	offline, record, input, source := offlineFixture(t)
	ctx := t.Context()
	// No JavDB client: state reads must remain entirely local.
	discover := &DiscoverService{database: offline.database, local: offline.drive}
	identity := []MovieIdentity{{ID: input.JavDBID, Code: input.Code}}
	assertState := func(want MovieState, libraryID int) {
		t.Helper()
		states, err := discover.MovieStates(ctx, identity)
		if err != nil || len(states) != 1 || states[0].ID != input.JavDBID ||
			states[0].State != want || states[0].LibraryID != libraryID {
			t.Fatalf("want %s with library %d, states=%+v err=%v", want, libraryID, states, err)
		}
	}
	assertState(MovieSaving, 0)
	sess, err := offline.drive.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := offline.updateTask(ctx, sess, record, pan.OfflineTask{Status: 2}); err != nil {
		t.Fatal(err)
	}
	assertState(MovieProcessing, 0)
	if err := offline.updateTask(ctx, sess, record, pan.OfflineTask{Status: 2, FileID: "download-folder"}); err != nil {
		t.Fatal(err)
	}
	assertState(MovieProcessing, 0)
	download := offline.database.Task.GetX(ctx, record.ID)
	saved, err := tasks.DecodePayload[offlinePayload](download.Payload)
	if err != nil {
		t.Fatal(err)
	}
	scanTask := offline.database.Task.GetX(ctx, saved.ScanTaskID)
	payload, err := tasks.DecodePayload[scan.Payload](scanTask.Payload)
	if err != nil {
		t.Fatal(err)
	}
	video := scan.IdentifyVideo(pan.File{ID: "video-1", ParentID: "download-folder", Name: input.Code + ".mp4", Size: 1 << 30})
	for range 2 {
		if err := scan.ProcessScanPage(ctx, offline.database, scanTask.ID, "first-page", "/Movies/download-folder", []scan.Video{video}, &payload, nil, offline.tasks); err != nil {
			t.Fatal(err)
		}
	}
	indexed := offline.database.File.Query().Where(file.FileIDEQ(video.ID)).OnlyX(ctx)
	assertState(MovieInLibrary, *indexed.MovieID)
	activity, err := offline.Activity(ctx)
	if err != nil || len(activity.Tasks) != 1 || activity.Tasks[0].Phase != "in_library" ||
		!activity.Tasks[0].Processing || activity.Tasks[0].LibraryID != *indexed.MovieID {
		t.Fatalf("indexed download is not playable during the scan: %+v err=%v", activity, err)
	}
	saved, err = tasks.DecodePayload[offlinePayload](offline.database.Task.GetX(ctx, record.ID).Payload)
	if err != nil || len(saved.FileIDs) != 1 || saved.FileIDs[0] != video.ID {
		t.Fatalf("committed files are missing or duplicated: %+v err=%v", saved, err)
	}
	// A failed metadata job does not revoke playback of an indexed video.
	metadata, err := tasks.EncodePayload(scrape.MetadataPayload{Source: source, ScanTaskID: scanTask.ID,
		MovieID: *indexed.MovieID, Code: input.Code, JavDBID: input.JavDBID})
	if err != nil {
		t.Fatal(err)
	}
	scrape := offline.database.Task.Create().SetType("scrape").SetPayload(metadata).SaveX(ctx)
	if err := offline.tasks.Queue().Finish(ctx, scanTask.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := offline.tasks.Queue().Finish(ctx, scrape.ID, errors.New("metadata unavailable")); err != nil {
		t.Fatal(err)
	}
	assertState(MovieInLibrary, *indexed.MovieID)
	activity, err = offline.Activity(ctx)
	if err != nil || len(activity.Tasks) != 1 || activity.Tasks[0].Phase != "in_library" ||
		activity.Tasks[0].Processing || activity.Tasks[0].Error == nil {
		t.Fatalf("metadata failure hid playback or completion: %+v err=%v", activity, err)
	}
	offline.database.File.DeleteOne(indexed).ExecX(ctx)
	assertState(MovieNotInLibrary, 0)
}

func TestMovieStatesScopePendingWorkToTheMountedAccountAndRoot(t *testing.T) {
	offline, record, input, source := offlineFixture(t)
	ctx := t.Context()
	discover := &DiscoverService{database: offline.database, local: offline.drive}
	// A completed download may await scan creation after its mount returns.
	input.FileID = "download-folder"
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		t.Fatal(err)
	}
	offline.database.Task.UpdateOne(record).SetStatus(task.StatusDone).SetPayload(encoded).ExecX(ctx)
	for _, scenario := range []struct {
		name, accountID, directoryID string
		want                         MovieState
	}{
		{"current source", source.AccountID, source.Directory.ID, MovieProcessing},
		{"other account", "another-account", source.Directory.ID, MovieNotInLibrary},
		{"other root", source.AccountID, "another-root", MovieNotInLibrary},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			mountSource(t, offline.drive, stubOf(t, offline.drive), domain.LibrarySource{
				AccountID: scenario.accountID, Directory: domain.LibraryDirectory{ID: scenario.directoryID, Name: "Root", Path: "/Root"},
			})
			states, err := discover.MovieStates(ctx, []MovieIdentity{{ID: input.JavDBID, Code: input.Code}})
			if err != nil || len(states) != 1 || states[0].State != scenario.want || states[0].LibraryID != 0 {
				t.Fatalf("state crossed its source: %+v err=%v", states, err)
			}
		})
	}
	if err := offline.drive.ClearDirectory(ctx); err != nil {
		t.Fatal(err)
	}
	states, err := discover.MovieStates(ctx, []MovieIdentity{{ID: input.JavDBID, Code: input.Code}})
	if err != nil || len(states) != 1 || states[0].State != MovieNotInLibrary {
		t.Fatalf("unmounted state: %+v err=%v", states, err)
	}
}

func TestOfflinePageFileTrackingRollsBackWithTheIndex(t *testing.T) {
	offline, record, input, source := offlineFixture(t)
	ctx := t.Context()
	payload := scan.Payload{Source: source, OfflineTaskID: record.ID, TargetID: "download-folder",
		Code: input.Code, JavDBID: input.JavDBID}
	video := scan.IdentifyVideo(pan.File{ID: "video", ParentID: "download-folder", Name: input.Code + ".mp4", Size: 1 << 30})
	// The missing parent fails the final progress write after file tracking.
	if err := scan.ProcessScanPage(ctx, offline.database, -1, "rolled-back", "/Movies/download-folder",
		[]scan.Video{video}, &payload, nil, offline.tasks); err == nil {
		t.Fatal("page with a missing scan parent unexpectedly committed")
	}
	if offline.database.File.Query().CountX(ctx) != 0 {
		t.Fatal("file index escaped rollback")
	}
	saved, err := tasks.DecodePayload[offlinePayload](offline.database.Task.GetX(ctx, record.ID).Payload)
	if err != nil || len(saved.FileIDs) != 0 {
		t.Fatalf("download file tracking escaped rollback: %+v err=%v", saved, err)
	}
}
