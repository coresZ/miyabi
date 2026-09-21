package service

import (
	"context"
	"fmt"
	"github.com/ppxb/miyabi/internal/tasks"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scan"
)

type MovieIdentity struct {
	ID   string `json:"id" binding:"required,max=200"`
	Code string `json:"code" binding:"required,max=200"`
}

type DiscoverMovieState struct {
	ID        string     `json:"id"`
	LibraryID int        `json:"library_id,omitempty"`
	State     MovieState `json:"state"`
}

// MovieStates only reads the local index and tasks. Catalogue cache lifetimes
// and upstream availability must not delay admission badges or playback.
func (service *DiscoverService) MovieStates(ctx context.Context, identities []MovieIdentity) ([]DiscoverMovieState, error) {
	result := make([]DiscoverMovieState, len(identities))
	if len(identities) == 0 {
		return result, nil
	}
	ids := make([]string, len(identities))
	codes := make([]string, len(identities))
	for index, item := range identities {
		ids[index], codes[index] = item.ID, codeid.Normalize(item.Code)
		result[index] = DiscoverMovieState{ID: item.ID, State: MovieNotInLibrary}
	}
	var source *domain.LibrarySource
	if service.local != nil {
		source = service.local.Source()
	}
	if source == nil {
		return result, nil
	}
	localMovies, err := service.database.Movie.Query().Where(
		movie.Or(movie.JavdbIDIn(ids...), movie.And(movie.JavdbIDIsNil(), movie.CodeIn(codes...))),
		movie.HasFilesWith(scan.LibraryFiles(*source)),
	).Select(movie.FieldID, movie.FieldCode, movie.FieldJavdbID).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query local movie states: %w", err)
	}
	byID, byCode := make(map[string]int), make(map[string]int)
	for _, record := range localMovies {
		if record.JavdbID != nil {
			byID[*record.JavdbID] = record.ID
		} else {
			byCode[record.Code] = record.ID
		}
	}
	var taskIDs []any
	for index, identity := range identities {
		item := &result[index]
		item.LibraryID = byID[identity.ID]
		if item.LibraryID == 0 {
			item.LibraryID = byCode[codes[index]]
		}
		if item.LibraryID != 0 {
			item.State = MovieInLibrary
		} else {
			taskIDs = append(taskIDs, identity.ID)
		}
	}
	if len(taskIDs) == 0 {
		return result, nil
	}

	var active []struct {
		Type    string      `json:"type"`
		Status  task.Status `json:"status"`
		JavDBID string      `json:"javdb_id"`
	}
	err = service.database.Task.Query().Where(task.Or(
		task.And(task.TypeEQ(tasks.KindOffline.String()), func(s *sql.Selector) {
			s.Where(sql.And(
				sqljson.ValueIn(task.FieldPayload, taskIDs, sqljson.Path(tasks.PathJavDBID)),
				sqljson.ValueEQ(task.FieldPayload, source.AccountID, sqljson.Path(tasks.PathAccountID)),
				sqljson.ValueEQ(task.FieldPayload, source.Directory.ID, sqljson.Path(tasks.PathDirectoryID)),
				sql.Or(sql.In(task.FieldStatus, string(task.StatusQueued), string(task.StatusRunning)),
					sql.And(sql.EQ(task.FieldStatus, string(task.StatusDone)),
						sql.Not(sqljson.HasKey(task.FieldPayload, sqljson.Path(tasks.PathScanTaskID))),
						sql.Or(
							sqljson.ValueNEQ(task.FieldPayload, "", sqljson.Path(tasks.PathFileID)),
							sqljson.ValueEQ(task.FieldPayload, true, sqljson.Path(tasks.PathAwaitingLocation)),
						),
					)),
			))
		}),
		task.And(task.TypeIn(tasks.KindScan.String(), tasks.KindScrape.String(), tasks.KindCover.String()), task.StatusIn(task.StatusQueued, task.StatusRunning), func(s *sql.Selector) {
			s.Where(sql.And(
				sqljson.ValueIn(task.FieldPayload, taskIDs, sqljson.Path(tasks.PathJavDBID)),
				sqljson.ValueEQ(task.FieldPayload, source.AccountID, sqljson.Path(tasks.PathSource, tasks.PathAccountID)),
				sqljson.ValueEQ(task.FieldPayload, source.Directory.ID, sqljson.Path(tasks.PathSource, "directory", "id")),
			))
		}),
	), func(s *sql.Selector) {
		// Cover payloads can contain full NFO documents; only read task identity.
		s.Select(s.C(task.FieldType), s.C(task.FieldStatus),
			sql.As(tasks.JSONExtract(s.C(task.FieldPayload), tasks.PathJavDBID), "javdb_id"))
	}).Select(task.FieldID).Scan(ctx, &active)
	if err != nil {
		return nil, fmt.Errorf("query movie workflows: %w", err)
	}
	saving, processing := make(map[string]bool), make(map[string]bool)
	for _, record := range active {
		if record.Type == tasks.KindOffline.String() && record.Status != task.StatusDone {
			saving[record.JavDBID] = true
		} else {
			processing[record.JavDBID] = true
		}
	}
	for index, identity := range identities {
		item := &result[index]
		if item.LibraryID != 0 {
			continue
		}
		switch {
		case processing[identity.ID]:
			item.State = MovieProcessing
		case saving[identity.ID]:
			item.State = MovieSaving
		}
	}
	return result, nil
}
