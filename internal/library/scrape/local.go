package scrape

import (
	"context"
	"fmt"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

// scrapeLocal resolves metadata and artwork like a remote scrape, but publishes
// into the central database only. Local files stay untouched, so the scraped
// result lives in data/miyabi.db and data/images as the single copy.
func (service *Service) scrapeLocal(ctx context.Context, job tasks.Job, input Payload) error {
	current, err := service.db.Movie.Query().Where(movie.IDEQ(input.MovieID)).Select(movie.FieldID, movie.FieldManualCode).Only(ctx)
	if err != nil {
		return err
	}
	if current.ManualCode != input.ManualCode {
		return domain.E(domain.KindConflict, "影片番号已纠正，请使用新的刮削任务", nil)
	}
	if !input.MetadataReady {
		if err := service.prepareMetadata(ctx, service.commitLocal, job.ID, &input); err != nil {
			return err
		}
	}
	if err := tasks.Checkpoint(ctx, service.db); err != nil {
		return err
	}
	if err := service.prepareArtwork(ctx, job.ID, &input); err != nil {
		return err
	}
	if err := tasks.Checkpoint(ctx, service.db); err != nil {
		return err
	}
	return service.publishLocal(ctx, job, input)
}

func (service *Service) commitLocal(ctx context.Context, fn func(*ent.Tx) error) error {
	return ent.WithTx(ctx, service.db, fn)
}

func (service *Service) publishLocal(ctx context.Context, job tasks.Job, input Payload) error {
	artwork := *input.Artwork
	files, err := service.db.File.Query().Where(FileScope(input.Source), file.MovieIDEQ(input.MovieID)).All(ctx)
	if err != nil {
		return fmt.Errorf("load local movie files: %w", err)
	}
	if len(files) == 0 {
		return domain.E(domain.KindConflict, "媒体文件索引已变化，请重新扫描", nil)
	}
	videos := make([]pan.File, 0, len(files))
	for _, entry := range files {
		videos = append(videos, pan.File{ID: entry.FileID, ParentID: entry.ParentID, Name: entry.Name, Size: entry.Size, SHA1: entry.Sha1})
	}
	snapshot := &domain.MetadataSnapshot{
		Code:          input.Code,
		AccountID:     input.Source.AccountID,
		DirectoryID:   input.Source.Directory.ID,
		PosterVersion: input.PosterVersion,
		Videos:        VideoFingerprint(videos),
	}
	input.Completed = true
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		return err
	}
	if err := ent.WithTx(ctx, service.db, func(tx *ent.Tx) error {
		record, err := tx.Movie.Query().Where(movie.IDEQ(input.MovieID)).Select(movie.FieldID, movie.FieldManualCode).Only(ctx)
		if err != nil {
			return err
		}
		if record.ManualCode != input.ManualCode {
			return domain.E(domain.KindConflict, "影片番号已纠正，请使用新的刮削任务", nil)
		}
		update := tx.Movie.UpdateOneID(input.MovieID).SetCode(input.Code).SetMetadata(&input.Document).
			SetCover(artwork.Thumbnail).SetPoster(artwork.Poster).SetFanarts([]string{artwork.Fanart}).
			SetScrapeStatus(movie.ScrapeStatusDone).SetMetadataSnapshot(snapshot)
		if id := input.Document.JavDBID(); id != "" {
			update.SetJavdbID(id)
		}
		if err := update.Exec(ctx); err != nil {
			return err
		}
		return tx.Task.UpdateOneID(job.ID).SetPayload(encoded).Exec(ctx)
	}); err != nil {
		return fmt.Errorf("publish local movie: %w", err)
	}
	if service.notifier != nil {
		service.notifier.NotifyLibraryChanged()
	}
	return nil
}
