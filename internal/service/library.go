package service

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/actor"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/predicate"
	"github.com/ppxb/miyabi/internal/ent/tag"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/tasks"
)

type LibraryEntity struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

type LibraryTag struct {
	ID      int    `json:"id"`
	JavDBID string `json:"javdb_id"`
	Name    string `json:"name"`
}

type LibraryMovie struct {
	ID           int                `json:"id"`
	Code         string             `json:"code"`
	Title        string             `json:"title"`
	JavDBID      *string            `json:"javdb_id,omitempty"`
	Cover        *string            `json:"cover,omitempty"`
	Poster       *string            `json:"poster,omitempty"`
	Fanart       string             `json:"fanart,omitempty"`
	ReleaseDate  string             `json:"release_date,omitempty"`
	Duration     int                `json:"duration"`
	Rating       float64            `json:"rating"`
	Director     *LibraryEntity     `json:"director,omitempty"`
	Maker        *LibraryEntity     `json:"maker,omitempty"`
	Series       *LibraryEntity     `json:"series,omitempty"`
	Actors       []LibraryEntity    `json:"actors"`
	Tags         []LibraryTag       `json:"tags"`
	ScrapeStatus movie.ScrapeStatus `json:"scrape_status"`
	Watched      bool               `json:"watched"`
}

type LibraryPage struct {
	Source  *domain.LibrarySource `json:"source,omitempty"`
	Movies  []LibraryMovie        `json:"movies"`
	Total   int                   `json:"total"`
	Page    int                   `json:"page"`
	HasMore bool                  `json:"has_more"`
}

type LibraryFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type LibraryService struct {
	images   *mediaimage.Cache
	database *ent.Client
	drive    *drive.Drive
	tasks    *tasks.Service
}

func NewLibraryService(database *ent.Client, d *drive.Drive, tasks *tasks.Service, images *mediaimage.Cache) *LibraryService {
	svc := &LibraryService{database: database, drive: d, tasks: tasks, images: images}
	if d != nil && tasks != nil {
		// A mount is complete only once its scan is queued; an error here makes
		// the drive roll the mount back.
		d.SubscribeMount(func(ctx context.Context, event drive.MountEvent) error {
			if event.Source.Directory.ID != "" {
				if _, err := tasks.EnqueueFreshScan(ctx, event.Source); err != nil {
					return err
				}
			}
			tasks.NotifyLibraryChanged()
			return nil
		})
	}
	return svc
}

func libraryFiles(source domain.LibrarySource) predicate.File {
	return file.And(file.AccountIDEQ(source.AccountID), file.RootIDEQ(source.Directory.ID))
}

func (service *LibraryService) Movies(ctx context.Context, page, limit int) (LibraryPage, error) {
	result := LibraryPage{Movies: []LibraryMovie{}, Page: page}
	source := service.drive.Source()
	if source == nil {
		return result, nil
	}
	result.Source = source
	scope := libraryFiles(*source)
	var err error
	result.Total, err = service.database.File.Query().Where(scope).Aggregate(func(s *sql.Selector) string {
		return sql.As("COUNT(DISTINCT "+s.C(file.FieldMovieID)+")", "total")
	}).Int(ctx)
	if err != nil {
		return result, fmt.Errorf("count library index: %w", err)
	}
	if result.Total == 0 {
		return result, nil
	}
	records, err := service.database.Movie.Query().Where(movie.HasFilesWith(scope)).
		Select(movie.FieldID, movie.FieldCode, movie.FieldTitle, movie.FieldJavdbID, movie.FieldCover, movie.FieldPoster,
			movie.FieldFanarts, movie.FieldReleaseDate, movie.FieldDuration, movie.FieldRating,
			movie.FieldDirectorID, movie.FieldDirectorName, movie.FieldMakerID, movie.FieldMakerName,
			movie.FieldSeriesID, movie.FieldSeriesName,
			movie.FieldScrapeStatus, movie.FieldWatched).
		Order(ent.Desc(movie.FieldCreatedAt), ent.Desc(movie.FieldID)).
		Offset((page - 1) * limit).Limit(limit).
		WithActors(func(query *ent.ActorQuery) {
			query.Select(actor.FieldID, actor.FieldJavdbID, actor.FieldName).Order(ent.Asc(actor.FieldName), ent.Asc(actor.FieldID))
		}).
		WithTags(func(query *ent.TagQuery) {
			query.Select(tag.FieldID, tag.FieldJavdbID, tag.FieldName).Order(ent.Asc(tag.FieldName), ent.Asc(tag.FieldID))
		}).All(ctx)
	if err != nil {
		return result, fmt.Errorf("list library movies: %w", err)
	}
	for _, record := range records {
		item := LibraryMovie{
			ID: record.ID, Code: record.Code, Title: record.Title,
			JavDBID: record.JavdbID, Cover: record.Cover, Poster: record.Poster,
			Duration: valueOrZero(record.Duration), Rating: valueOrZero(record.Rating),
			Director: libraryEntity(record.DirectorID, record.DirectorName),
			Maker:    libraryEntity(record.MakerID, record.MakerName), Series: libraryEntity(record.SeriesID, record.SeriesName),
			Actors: make([]LibraryEntity, 0, len(record.Edges.Actors)),
			Tags:   make([]LibraryTag, 0, len(record.Edges.Tags)), ScrapeStatus: record.ScrapeStatus, Watched: record.Watched,
		}
		if record.ReleaseDate != nil {
			item.ReleaseDate = record.ReleaseDate.Format(time.DateOnly)
		}
		if len(record.Fanarts) > 0 {
			item.Fanart = record.Fanarts[0]
		}
		for _, person := range record.Edges.Actors {
			item.Actors = append(item.Actors, LibraryEntity{ID: person.JavdbID, Name: person.Name})
		}
		for _, label := range record.Edges.Tags {
			item.Tags = append(item.Tags, LibraryTag{ID: label.ID, JavDBID: label.JavdbID, Name: label.Name})
		}
		result.Movies = append(result.Movies, item)
	}
	result.HasMore = (page-1)*limit+len(result.Movies) < result.Total
	return result, nil
}

func libraryEntity(id, name *string) *LibraryEntity {
	if name == nil || *name == "" {
		return nil
	}
	return &LibraryEntity{ID: valueOrZero(id), Name: *name}
}

func valueOrZero[T any](value *T) T {
	if value != nil {
		return *value
	}
	var zero T
	return zero
}
