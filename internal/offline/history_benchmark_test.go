package offline

import (
	"fmt"
	"testing"

	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/tasks"
)

var offlineBenchmarkResult any

func BenchmarkOfflineActivityHistory(b *testing.B) {
	service, _, _, payload := offlineFixture(b)
	if err := ent.WithTx(b.Context(), service.database, func(tx *ent.Tx) error {
		for batch := range 10 {
			var jobs []*ent.TaskCreate
			for i := range 500 {
				input, err := tasks.EncodePayload(offlinePayload{
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
		offlineBenchmarkResult = activity
	}
}
