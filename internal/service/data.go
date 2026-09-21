package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"entgo.io/ent/dialect/sql"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/task"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/tasks"
)

var ErrCacheBusy = domain.E(domain.KindBusy, "封面正在处理或缓存正在清理，请稍后重试", nil)

type DataInfo struct {
	DataDirectory     string                `json:"data_directory"`
	DatabaseSizeBytes int64                 `json:"database_size_bytes"`
	Cache             mediaimage.CacheStats `json:"cache"`
}

type DataService struct {
	directory string
	db        *ent.Client
	images    *mediaimage.Cache
	scrape    *scrape.Service
}

func NewDataService(directory string, db *ent.Client, images *mediaimage.Cache, scrapeSvc *scrape.Service) (*DataService, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	return &DataService{directory: absolute, db: db, images: images, scrape: scrapeSvc}, nil
}

func (service *DataService) Info(ctx context.Context) (DataInfo, error) {
	retained, err := service.retainedArtwork(ctx)
	if err != nil {
		return DataInfo{}, err
	}
	return service.info(ctx, retained)
}

func (service *DataService) ClearCache(ctx context.Context) (DataInfo, error) {
	if err := ctx.Err(); err != nil {
		return DataInfo{}, err
	}
	if !service.scrape.TryLockArtwork() {
		return DataInfo{}, ErrCacheBusy
	}
	defer service.scrape.UnlockArtwork()

	retained, err := service.retainedArtwork(ctx)
	if err != nil {
		return DataInfo{}, err
	}
	if err := service.images.Prune(ctx, retained); err != nil {
		return DataInfo{}, err
	}
	return service.info(ctx, retained)
}

func (service *DataService) info(ctx context.Context, retained map[string]bool) (DataInfo, error) {
	result := DataInfo{DataDirectory: service.directory}
	var err error
	result.Cache, err = service.images.Stats(ctx, retained)
	if err != nil {
		return DataInfo{}, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := ctx.Err(); err != nil {
			return DataInfo{}, err
		}
		info, err := os.Stat(filepath.Join(service.directory, "miyabi.db"+suffix))
		if suffix != "" && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return DataInfo{}, fmt.Errorf("read database size: %w", err)
		}
		if !info.Mode().IsRegular() {
			return DataInfo{}, errors.New("database path is not a regular file")
		}
		result.DatabaseSizeBytes += info.Size()
	}
	return result, nil
}

func (service *DataService) retainedArtwork(ctx context.Context) (map[string]bool, error) {
	// Include every account and directory, including films temporarily without
	// indexed files. Switching the active library must not make their covers disposable.
	records, err := service.db.Movie.Query().
		Select(movie.FieldID, movie.FieldCover, movie.FieldPoster, movie.FieldFanarts).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read artwork references: %w", err)
	}
	retained := make(map[string]bool)
	for _, record := range records {
		retained[valueOrZero(record.Cover)] = true
		retained[valueOrZero(record.Poster)] = true
		for _, fanart := range record.Fanarts {
			retained[fanart] = true
		}
	}

	// Failed and queued cover jobs can resume from locally saved artwork before
	// the movie references it. Extract only those URLs, not the full NFO payloads.
	var pending []mediaimage.Artwork
	err = service.db.Task.Query().Where(
		task.TypeEQ(tasks.KindCover.String()), task.StatusNEQ(task.StatusDone),
		func(selector *sql.Selector) {
			selector.Select(
				"coalesce("+tasks.JSONExtract(task.FieldPayload, tasks.PathArtwork, "poster")+", '') AS poster",
				"coalesce("+tasks.JSONExtract(task.FieldPayload, tasks.PathArtwork, "fanart")+", '') AS fanart",
				"coalesce("+tasks.JSONExtract(task.FieldPayload, tasks.PathArtwork, "thumbnail")+", '') AS thumbnail",
			)
		},
	).Select(task.FieldID).Scan(ctx, &pending)
	if err != nil {
		return nil, fmt.Errorf("read pending artwork references: %w", err)
	}
	for _, artwork := range pending {
		retained[artwork.Poster] = true
		retained[artwork.Fanart] = true
		retained[artwork.Thumbnail] = true
	}
	return retained, nil
}

func valueOrZero[T any](value *T) T {
	if value != nil {
		return *value
	}
	var zero T
	return zero
}
