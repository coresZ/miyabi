package scan

import (
	"context"
	"fmt"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/tasks"
)

// EnqueueLocalScrapes queues metadata jobs for indexed local movies that still
// need scraping. A local import without an NFO only registers the code, so the
// scrape service fills the central database instead of writing sidecars beside
// the media files.
func EnqueueLocalScrapes(ctx context.Context, db *ent.Client, taskID int, source domain.LibrarySource) (int, error) {
	records, err := db.Movie.Query().Where(
		movie.HasFilesWith(scrape.FileScope(source)),
		movie.ScrapeStatusNEQ(movie.ScrapeStatusDone),
	).Select(movie.FieldID, movie.FieldCode, movie.FieldManualCode).Order(movie.ByID()).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("load local movies for scraping: %w", err)
	}
	if len(records) == 0 {
		return 0, nil
	}
	keys := make([]string, len(records))
	for i, record := range records {
		keys[i] = fmt.Sprintf("movie:%d", record.ID)
	}
	active, err := db.Task.Query().Where(
		task.TypeEQ(string(tasks.KindScrape)),
		task.ResourceKeyIn(keys...),
		task.StatusIn(task.StatusQueued, task.StatusRunning),
	).Select(task.FieldResourceKey).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("load active local scrape tasks: %w", err)
	}
	busy := make(map[string]bool, len(active))
	for _, item := range active {
		busy[item.ResourceKey] = true
	}
	var queued int
	err = ent.WithTx(ctx, db, func(tx *ent.Tx) error {
		builders := make([]*ent.TaskCreate, 0, len(records))
		for _, record := range records {
			key := fmt.Sprintf("movie:%d", record.ID)
			if busy[key] {
				continue
			}
			payload, err := tasks.EncodePayload(scrape.MetadataPayload{
				Source: source, ScanTaskID: taskID, MovieID: record.ID,
				Code: record.Code, ManualCode: record.ManualCode,
			})
			if err != nil {
				return err
			}
			builders = append(builders, tx.Task.Create().SetType(tasks.KindScrape.String()).
				SetResourceKey(key).SetPayload(payload))
		}
		if len(builders) == 0 {
			return nil
		}
		if err := tx.Task.CreateBulk(builders...).Exec(ctx); err != nil {
			return fmt.Errorf("enqueue local movie metadata: %w", err)
		}
		queued = len(builders)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return queued, nil
}
