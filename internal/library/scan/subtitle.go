package scan

import (
	"context"
	"path"
	"strings"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/pan"
	subpkg "github.com/ppxb/miyabi/internal/subtitle"
)

// IndexDirectorySubtitles resolves and registers subtitle files found in the directory.
func IndexDirectorySubtitles(ctx context.Context, tx *ent.Tx, videos []Video, subtitles []pan.File) error {
	if len(videos) == 0 || len(subtitles) == 0 {
		return nil
	}

	// Map each video code to movie_id
	codes := make(map[string]int)
	for _, v := range videos {
		if v.Code != "" {
			codes[v.Code] = 0
		}
	}
	if len(codes) == 0 {
		return nil
	}

	numbers := make([]string, 0, len(codes))
	for c := range codes {
		numbers = append(numbers, c)
	}
	movieRecords, err := tx.Movie.Query().Where(movie.CodeIn(numbers...)).Select(movie.FieldID, movie.FieldCode).All(ctx)
	if err != nil {
		return err
	}
	for _, m := range movieRecords {
		codes[m.Code] = m.ID
	}

	// If there is only one movie in this directory, it is an exclusive directory
	var exclusiveMovieID int
	if len(codes) == 1 {
		for _, id := range codes {
			exclusiveMovieID = id
		}
	}

	for _, sub := range subtitles {
		targetMovieID := 0
		subCode, _ := codeid.Parse(sub.Name)
		if subCode != "" {
			for c, id := range codes {
				if codeid.IsEquivalent(c, subCode) {
					targetMovieID = id
					break
				}
			}
		}
		if targetMovieID == 0 && exclusiveMovieID != 0 {
			targetMovieID = exclusiveMovieID
		}
		if targetMovieID == 0 {
			continue
		}

		exists, err := tx.Subtitle.Query().
			Where(subtitle.MovieIDEQ(targetMovieID), subtitle.FileIDEQ(sub.ID)).
			Exist(ctx)
		if err != nil || exists {
			continue
		}

		ext := strings.ToLower(strings.TrimPrefix(path.Ext(sub.Name), "."))
		lang := subpkg.DetectChineseLanguage(sub.Name, "")
		ver := subpkg.DetectVersion(sub.Name)
		displayName := subpkg.BuildDisplayName(lang, ver, true)

		hasDefault, _ := tx.Subtitle.Query().
			Where(subtitle.MovieIDEQ(targetMovieID), subtitle.IsDefault(true)).
			Exist(ctx)

		_ = tx.Subtitle.Create().
			SetMovieID(targetMovieID).
			SetFileID(sub.ID).
			SetPickCode(sub.PickCode).
			SetName(sub.Name).
			SetDisplayName(displayName).
			SetLanguage(string(lang)).
			SetFormat(ext).
			SetVersionTag(string(ver)).
			SetSource("local").
			SetOffsetMs(0).
			SetIsDefault(!hasDefault).
			Exec(ctx)
	}
	return nil
}
