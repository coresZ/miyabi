package playback

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/ent/watchhistory"
	"github.com/ppxb/miyabi/internal/pan"
)

func (service *Service) Files(ctx context.Context, movieID int) (PlayFiles, error) {
	source := service.drive.Source()
	if source == nil {
		return PlayFiles{}, drive.ErrMediaDirectoryRequired
	}
	scope := database.LibraryFiles(*source)
	record, err := service.database.Movie.Query().
		Where(movie.IDEQ(movieID), movie.HasFilesWith(scope)).
		WithFiles(func(query *ent.FileQuery) {
			query.Where(scope).Order(ent.Desc(file.FieldSize), ent.Asc(file.FieldPath), ent.Asc(file.FieldID))
		}).Only(ctx)
	if err != nil {
		return PlayFiles{}, fmt.Errorf("read playable movie: %w", err)
	}
	result := PlayFiles{Code: record.Code, Title: record.Title, Files: make([]domain.LibraryFile, 0, len(record.Edges.Files))}
	for _, entry := range record.Edges.Files {
		result.Files = append(result.Files, domain.LibraryFile{ID: entry.FileID, Name: entry.Name, Path: entry.Path, Size: entry.Size})
	}
	result.Source = domain.WatchHistoryScope{AccountID: source.AccountID, DirectoryID: source.Directory.ID}
	history, err := service.database.WatchHistory.Query().
		Where(database.WatchHistory(*source), watchhistory.MovieIDEQ(movieID)).
		Select(watchhistory.FieldID, watchhistory.FieldFileID, watchhistory.FieldPosition, watchhistory.FieldDuration).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return PlayFiles{}, fmt.Errorf("read playback resume: %w", err)
	}
	if history != nil {
		result.Resume = &domain.WatchResume{ID: history.ID, FileID: history.FileID, Position: history.Position, Duration: history.Duration}
	}
	subtitles, err := service.database.Subtitle.Query().
		Where(subtitle.MovieIDEQ(movieID)).
		Order(ent.Desc(subtitle.FieldIsDefault), ent.Asc(subtitle.FieldID)).
		All(ctx)
	if err == nil && len(subtitles) > 0 {
		result.Subtitles = make([]domain.SubtitleTrack, 0, len(subtitles))
		for _, sub := range subtitles {
			result.Subtitles = append(result.Subtitles, domain.SubtitleTrack{
				ID:          sub.ID,
				MovieID:     sub.MovieID,
				FileID:      sub.FileID,
				Name:        sub.Name,
				DisplayName: sub.DisplayName,
				Language:    sub.Language,
				Format:      sub.Format,
				VersionTag:  sub.VersionTag,
				Source:      sub.Source,
				OffsetMs:    sub.OffsetMs,
				IsDefault:   sub.IsDefault,
				Src:         fmt.Sprintf("/api/play/subtitles/%d.vtt", sub.ID),
			})
		}
	}
	return result, nil
}

func (service *Service) Start(ctx context.Context, fileID string) (Playback, error) {
	sess, err := service.drive.Open(ctx)
	if err != nil {
		return Playback{}, err
	}
	source := sess.Source()
	_, err = service.database.File.Query().Where(database.LibraryFiles(source), file.FileIDEQ(fileID)).Only(ctx)
	if err != nil {
		return Playback{}, fmt.Errorf("read indexed video: %w", err)
	}
	info, err := sess.Info(ctx, fileID)
	if err != nil {
		return Playback{}, fmt.Errorf("read 115 video: %w", err)
	}
	if info.IsDirectory || !domain.IsVideo(info.Name) || !drive.WithinSource(info, source) {
		return Playback{}, domain.E(domain.KindNotFound, "视频已不在当前媒体目录中，请重新扫描", fs.ErrNotExist)
	}
	if info.PickCode == "" {
		return Playback{}, fmt.Errorf("115 returned no pick code for video")
	}
	sources, err := sess.PlayURL(ctx, info.PickCode)
	if err != nil {
		// pan reports the raw condition; the user-facing text lives in drive.
		if errors.Is(err, pan.ErrTranscodeUnavailable) {
			return Playback{}, drive.ErrTranscodeUnavailable
		}
		return Playback{}, fmt.Errorf("get 115 playback URL: %w", err)
	}
	return service.createSession(source, sess.Version(), sources)
}
