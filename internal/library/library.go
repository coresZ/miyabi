package library

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
	"github.com/ppxb/miyabi/internal/ent/tag"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/library/scan"
	"github.com/ppxb/miyabi/internal/tasks"
)

type Entity struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

type Tag struct {
	ID      int    `json:"id"`
	JavDBID string `json:"javdb_id"`
	Name    string `json:"name"`
}

type Movie struct {
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
	Director     *Entity            `json:"director,omitempty"`
	Maker        *Entity            `json:"maker,omitempty"`
	Series       *Entity            `json:"series,omitempty"`
	Actors       []Entity           `json:"actors"`
	Tags         []Tag              `json:"tags"`
	ScrapeStatus movie.ScrapeStatus `json:"scrape_status"`
	Watched      bool               `json:"watched"`
}

type Page struct {
	Source  *domain.LibrarySource `json:"source,omitempty"`
	Movies  []Movie               `json:"movies"`
	Total   int                   `json:"total"`
	Page    int                   `json:"page"`
	HasMore bool                  `json:"has_more"`
}

type File struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Aliases for compatibility
type LibraryEntity = Entity
type LibraryTag = Tag
type LibraryMovie = Movie
type LibraryPage = Page
type LibraryFile = File

type Service struct {
	images   *mediaimage.Cache
	database *ent.Client
	drive    *drive.Drive
	tasks    *tasks.Service
}

func New(database *ent.Client, d *drive.Drive, tasks *tasks.Service, images *mediaimage.Cache) *Service {
	svc := &Service{database: database, drive: d, tasks: tasks, images: images}
	if d != nil && tasks != nil {
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
	if database != nil {
		_ = migrateViewedMovies(context.Background(), database)
	}
	return svc
}

func (s *Service) Database() *ent.Client {
	return s.database
}

func (s *Service) Drive() *drive.Drive {
	return s.drive
}

func (s *Service) Tasks() *tasks.Service {
	return s.tasks
}

func (s *Service) Images() *mediaimage.Cache {
	return s.images
}

func (s *Service) StartScan(ctx context.Context) (tasks.TaskInfo, error) {
	sess, err := s.drive.Open(ctx)
	if err != nil {
		return tasks.TaskInfo{}, err
	}
	return s.tasks.EnqueueScan(ctx, sess.Source())
}

func (s *Service) Scan(ctx context.Context, job tasks.Job) error {
	return scan.Scan(ctx, job, s.drive, s.database, s.images, s.tasks)
}

func (s *Service) Finished(context.Context, *ent.Tx, tasks.Job, error) (tasks.Change, error) {
	return tasks.ChangeOffline, nil
}

func (s *Service) Movies(ctx context.Context, page, limit int) (Page, error) {
	result := Page{Movies: []Movie{}, Page: page}
	source := s.drive.Source()
	if source == nil {
		return result, nil
	}
	result.Source = source
	scope := scan.LibraryFiles(*source)
	var err error
	result.Total, err = s.database.File.Query().Where(scope).Aggregate(func(selector *sql.Selector) string {
		return sql.As("COUNT(DISTINCT "+selector.C(file.FieldMovieID)+")", "total")
	}).Int(ctx)
	if err != nil {
		return result, fmt.Errorf("count library index: %w", err)
	}
	if result.Total == 0 {
		return result, nil
	}
	records, err := s.database.Movie.Query().Where(movie.HasFilesWith(scope)).
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
		item := Movie{
			ID: record.ID, Code: record.Code, Title: record.Title,
			JavDBID: record.JavdbID, Cover: record.Cover, Poster: record.Poster,
			Duration: valueOrZero(record.Duration), Rating: valueOrZero(record.Rating),
			Director: libraryEntity(record.DirectorID, record.DirectorName),
			Maker:    libraryEntity(record.MakerID, record.MakerName), Series: libraryEntity(record.SeriesID, record.SeriesName),
			Actors: make([]Entity, 0, len(record.Edges.Actors)),
			Tags:   make([]Tag, 0, len(record.Edges.Tags)), ScrapeStatus: record.ScrapeStatus, Watched: record.Watched,
		}
		if record.ReleaseDate != nil {
			item.ReleaseDate = record.ReleaseDate.Format(time.DateOnly)
		}
		if len(record.Fanarts) > 0 {
			item.Fanart = record.Fanarts[0]
		}
		for _, person := range record.Edges.Actors {
			item.Actors = append(item.Actors, Entity{ID: person.JavdbID, Name: person.Name})
		}
		for _, label := range record.Edges.Tags {
			item.Tags = append(item.Tags, Tag{ID: label.ID, JavDBID: label.JavdbID, Name: label.Name})
		}
		result.Movies = append(result.Movies, item)
	}
	result.HasMore = (page-1)*limit+len(result.Movies) < result.Total
	return result, nil
}

func libraryEntity(id, name *string) *Entity {
	if name == nil || *name == "" {
		return nil
	}
	return &Entity{ID: valueOrZero(id), Name: *name}
}

func valueOrZero[T any](value *T) T {
	if value != nil {
		return *value
	}
	var zero T
	return zero
}
