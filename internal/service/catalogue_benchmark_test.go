package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/tasks"
)

var catalogueBenchmarkResult any

func BenchmarkOfflineActivityHistory(b *testing.B) {
	service, _, _, payload := offlineFixture(b)
	if err := ent.WithTx(b.Context(), service.database, func(tx *ent.Tx) error {
		for batch := range 10 {
			var jobs []*ent.TaskCreate
			for i := range 500 {
				input, err := tasks.EncodePayload(offlineTaskPayload{
					Code: "ABP-001", JavDBID: "movie", Hash: fmt.Sprintf("%040d", i%50),
					InfoHash: fmt.Sprintf("%040d", i%50), AccountID: payload.AccountID,
					DirectoryID: payload.Directory.ID,
				})
				if err != nil {
					return err
				}
				jobs = append(jobs, tx.Task.Create().SetType("offline").SetStatus(task.StatusDone).
					SetProgress(batch*10).SetPayload(input))
			}
			if err := tx.Task.CreateBulk(jobs...).Exec(b.Context()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		activity, err := service.Activity(b.Context())
		if err != nil || len(activity.Tasks) != 50 {
			b.Fatalf("activity: %d tasks, %v", len(activity.Tasks), err)
		}
		catalogueBenchmarkResult = activity
	}
}

func BenchmarkTaskPayload(b *testing.B) {
	input := scrape.CoverPayload{
		MetadataPayload: scrape.MetadataPayload{Source: domain.LibrarySource{AccountID: "100", Directory: domain.LibraryDirectory{ID: "10", Path: "/Movies"}},
			ScanTaskID: 1, MovieID: 2, Code: "ABP-001", JavDBID: "movie"},
		Document: nfo.Movie{Code: "ABP-001", Title: "Fixture title", Rating: 4.5},
		Snapshot: &scrape.Snapshot{Videos: "fingerprint", Directories: []scrape.DirectorySnapshot{{ID: "10"}}},
	}
	for i := range 20 {
		input.Document.Tags = append(input.Document.Tags, nfo.Tag{ID: fmt.Sprint(i), Name: "Fixture tag", CategoryID: "category"})
	}
	body, err := json.Marshal(input)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			encoded, err := tasks.EncodePayload(input)
			if err != nil {
				b.Fatal(err)
			}
			stored, err := json.Marshal(encoded)
			if err != nil {
				b.Fatal(err)
			}
			catalogueBenchmarkResult = stored
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var record ent.Task
			if err := json.Unmarshal(body, &record.Payload); err != nil {
				b.Fatal(err)
			}
			decoded, err := tasks.DecodePayload[scrape.CoverPayload](record.Payload)
			if err != nil {
				b.Fatal(err)
			}
			catalogueBenchmarkResult = decoded
		}
	})
}
