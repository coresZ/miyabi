package library

import (
	"context"
	"fmt"
	"strings"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/predicate"
)

// Playable source kinds. Favorites is reserved for a future library feature.
const (
	PlayableLocal    = "local"
	PlayableRemote   = "remote"
	PlayableFavorite = "favorite"
)

type PlayableVideo struct {
	FileID string `json:"file_id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

// Playable is one movie with the videos a player can open for its source.
type Playable struct {
	MovieID int             `json:"movie_id"`
	Code    string          `json:"code"`
	Title   string          `json:"title"`
	Poster  *string         `json:"poster,omitempty"`
	Kind    string          `json:"kind"`
	Videos  []PlayableVideo `json:"videos"`
}

type PlayablePage struct {
	Items   []Playable            `json:"items"`
	Source  *domain.LibrarySource `json:"source,omitempty"`
	Page    int                   `json:"page"`
	HasMore bool                  `json:"has_more"`
}

// Playables lists the movies a player can open for one source scope. Local
// scope can be narrowed to a configured root; remote scope uses the mounted
// library source; favorites is reserved and returns nothing for now.
func (s *Service) Playables(ctx context.Context, scope, directory, query string, page, limit int) (PlayablePage, error) {
	result := PlayablePage{Items: []Playable{}, Page: page}
	if page < 1 {
		page, result.Page = 1, 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	var scopePred predicate.File
	switch scope {
	case PlayableLocal:
		scopePred = file.AccountIDEQ(domain.LocalAccountID)
		if directory != "" {
			root, err := s.localPlayRoot(ctx, directory)
			if err != nil {
				return result, err
			}
			scopePred = file.And(scopePred, file.RootIDEQ(root))
		}
	case PlayableRemote:
		source := s.Source()
		if source == nil {
			return result, nil
		}
		result.Source = source
		scopePred = database.LibraryFiles(*source)
	case PlayableFavorite:
		return result, nil
	default:
		return result, domain.E(domain.KindInvalid, "未知的播放来源", nil)
	}

	filters := []predicate.Movie{movie.HasFilesWith(scopePred)}
	if term := strings.TrimSpace(query); term != "" {
		filters = append(filters, movie.Or(movie.CodeContainsFold(term), movie.TitleContainsFold(term)))
	}
	ids, err := s.database.Movie.Query().Where(filters...).
		Order(ent.Desc(movie.FieldCreatedAt), ent.Desc(movie.FieldID)).
		Offset((page - 1) * limit).Limit(limit + 1).IDs(ctx)
	if err != nil {
		return result, fmt.Errorf("list playable movies: %w", err)
	}
	if len(ids) > limit {
		result.HasMore = true
		ids = ids[:limit]
	}
	if len(ids) == 0 {
		return result, nil
	}

	records, err := s.database.Movie.Query().Where(movie.IDIn(ids...)).
		Select(movie.FieldID, movie.FieldCode, movie.FieldTitle, movie.FieldPoster).All(ctx)
	if err != nil {
		return result, fmt.Errorf("load playable movies: %w", err)
	}
	byID := make(map[int]*ent.Movie, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}

	files, err := s.database.File.Query().Where(scopePred, file.MovieIDIn(ids...)).
		Order(ent.Asc(file.FieldName), ent.Asc(file.FieldID)).All(ctx)
	if err != nil {
		return result, fmt.Errorf("load playable videos: %w", err)
	}
	videos := make(map[int][]PlayableVideo, len(ids))
	for _, entry := range files {
		if entry.MovieID == nil || !domain.IsVideo(entry.Name) {
			continue
		}
		videos[*entry.MovieID] = append(videos[*entry.MovieID],
			PlayableVideo{FileID: entry.FileID, Name: entry.Name, Size: entry.Size})
	}
	for _, id := range ids {
		record, source := byID[id], videos[id]
		if record == nil || len(source) == 0 {
			continue
		}
		result.Items = append(result.Items, Playable{
			MovieID: record.ID, Code: record.Code, Title: record.Title,
			Poster: record.Poster, Kind: scope, Videos: source,
		})
	}
	return result, nil
}

// localPlayRoot resolves a configured local source id to its root path, and
// otherwise treats the value as a raw root path.
func (s *Service) localPlayRoot(ctx context.Context, directory string) (string, error) {
	if s.localSources != nil {
		source, found, err := s.localSources.Find(ctx, directory)
		if err != nil {
			return "", err
		}
		if found {
			return source.Path, nil
		}
	}
	return directory, nil
}
