package subtitle

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/subtitle"
	"github.com/ppxb/miyabi/internal/pan"
)

// Service coordinates subtitle discovery, retrieval, caching, and persistence.
type Service struct {
	db         *ent.Client
	aggregator *Aggregator
	drive      *drive.Drive
	cacheDir   string
	embyDir    string
}

// NewService creates a new subtitle service.
func NewService(db *ent.Client, aggregator *Aggregator, driveSvc *drive.Drive, dataDir string) (*Service, error) {
	cacheDir := filepath.Join(dataDir, "subtitles")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create subtitle cache directory: %w", err)
	}
	return &Service{
		db:         db,
		aggregator: aggregator,
		drive:      driveSvc,
		cacheDir:   cacheDir,
	}, nil
}

// SetEmbyDir sets the directory for exporting subtitles to local Emby media library.
func (s *Service) SetEmbyDir(embyDir string) {
	s.embyDir = embyDir
}

// ToTrack converts an ent.Subtitle record to a domain.SubtitleTrack.
func ToTrack(r *ent.Subtitle) domain.SubtitleTrack {
	return domain.SubtitleTrack{
		ID:          r.ID,
		MovieID:     r.MovieID,
		FileID:      r.FileID,
		Name:        r.Name,
		DisplayName: r.DisplayName,
		Language:    r.Language,
		Format:      r.Format,
		VersionTag:  r.VersionTag,
		Source:      r.Source,
		OffsetMs:    r.OffsetMs,
		IsDefault:   r.IsDefault,
		Src:         domain.SubtitleTrackURL(r.ID),
	}
}

// ToTracks converts a slice of ent.Subtitle records to domain.SubtitleTracks.
func ToTracks(records []*ent.Subtitle) []domain.SubtitleTrack {
	tracks := make([]domain.SubtitleTrack, len(records))
	for i, r := range records {
		tracks[i] = ToTrack(r)
	}
	return tracks
}

// ListByMovie retrieves all subtitle tracks associated with a movie ordered by default status.
func (s *Service) ListByMovie(ctx context.Context, movieID int) ([]domain.SubtitleTrack, error) {
	records, err := s.db.Subtitle.Query().
		Where(subtitle.MovieIDEQ(movieID)).
		Order(ent.Desc(subtitle.FieldIsDefault), ent.Asc(subtitle.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subtitles for movie %d: %w", movieID, err)
	}
	return ToTracks(records), nil
}

// Search queries online providers for subtitle candidates matching a movie code.
func (s *Service) Search(ctx context.Context, code string, isUncensored bool) ([]domain.SubtitleCandidate, error) {
	candidates, err := s.aggregator.Search(ctx, code, isUncensored)
	if err != nil {
		return nil, err
	}

	results := make([]domain.SubtitleCandidate, 0, len(candidates))
	for _, c := range candidates {
		results = append(results, domain.SubtitleCandidate{
			Source:      c.Provider,
			Name:        c.Name,
			DisplayName: c.DisplayName,
			Language:    string(c.Language),
			Version:     string(c.Version),
			URL:         c.URL,
			Ext:         c.Ext,
			Score:       c.Score,
		})
	}
	return results, nil
}

func (s *Service) cacheVTT(movieID int, content string) (string, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))[:12]
	fileName := fmt.Sprintf("%d_%s.vtt", movieID, hash)
	filePath := filepath.Join(s.cacheDir, fileName)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", err
	}
	return filePath, nil
}

type saveTrackInput struct {
	MovieID     int
	FileID      string
	PickCode    string
	Name        string
	DisplayName string
	Language    string
	VersionTag  string
	Source      string
	SourceURL   string
	StoragePath string
	IsDefault   bool
}

func (s *Service) saveTrackTx(ctx context.Context, tx *ent.Tx, input saveTrackInput) (*ent.Subtitle, error) {
	// 1. Remove any existing subtitle with the same language and version tag for this movie
	oldSubs, err := tx.Subtitle.Query().
		Where(
			subtitle.MovieIDEQ(input.MovieID),
			subtitle.LanguageEQ(input.Language),
			subtitle.VersionTagEQ(input.VersionTag),
		).All(ctx)
	if err == nil {
		for _, old := range oldSubs {
			_ = tx.Subtitle.DeleteOneID(old.ID).Exec(ctx)
			if old.StoragePath != "" && old.StoragePath != input.StoragePath {
				otherCount, _ := tx.Subtitle.Query().
					Where(subtitle.StoragePathEQ(old.StoragePath), subtitle.IDNEQ(old.ID)).
					Count(ctx)
				if otherCount == 0 {
					_ = os.Remove(old.StoragePath)
				}
			}
		}
	}

	// 2. Set previous subtitles as non-default if setting default
	if input.IsDefault {
		if err := tx.Subtitle.Update().
			Where(subtitle.MovieIDEQ(input.MovieID)).
			SetIsDefault(false).
			Exec(ctx); err != nil {
			return nil, err
		}
	}

	// 3. Insert the new subtitle track
	record, err := tx.Subtitle.Create().
		SetMovieID(input.MovieID).
		SetFileID(input.FileID).
		SetPickCode(input.PickCode).
		SetName(input.Name).
		SetDisplayName(input.DisplayName).
		SetLanguage(input.Language).
		SetFormat("vtt").
		SetVersionTag(input.VersionTag).
		SetSource(input.Source).
		SetSourceURL(input.SourceURL).
		SetStoragePath(input.StoragePath).
		SetOffsetMs(0).
		SetIsDefault(input.IsDefault).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create subtitle record: %w", err)
	}
	return record, nil
}

// ApplyCandidate downloads a candidate subtitle, converts it to WebVTT, caches it,
// optionally uploads it to 115 directory, and records it in the database while ensuring
// only one subtitle of the same language & version exists per movie.
func (s *Service) ApplyCandidate(ctx context.Context, movieID int, candidate domain.SubtitleCandidate) (*domain.SubtitleTrack, error) {
	vttContent, err := s.aggregator.DownloadAndConvert(ctx, Candidate{
		Provider:    candidate.Source,
		Name:        candidate.Name,
		URL:         candidate.URL,
		Ext:         candidate.Ext,
		Language:    Language(candidate.Language),
		Version:     VersionTag(candidate.Version),
		DisplayName: candidate.DisplayName,
	})
	if err != nil {
		return nil, fmt.Errorf("download and convert subtitle: %w", err)
	}

	filePath, err := s.cacheVTT(movieID, vttContent)
	if err != nil {
		return nil, fmt.Errorf("save subtitle cache: %w", err)
	}

	// Determine movie code and 115 directory for upload
	movieRecord, err := s.db.Movie.Get(ctx, movieID)
	if err != nil {
		return nil, fmt.Errorf("find movie %d: %w", movieID, err)
	}
	code := movieRecord.Code

	var directoryID string
	fileRecord, _ := s.db.File.Query().Where(file.MovieIDEQ(movieID)).First(ctx)
	if fileRecord != nil {
		directoryID = fileRecord.ParentID
	}

	subUploadName := fmt.Sprintf("%s.%s.vtt", code, candidate.Language)
	if candidate.Version != "" && candidate.Version != "standard" {
		subUploadName = fmt.Sprintf("%s.%s.%s.vtt", code, candidate.Version, candidate.Language)
	}

	var fileID string
	var pickCode string
	if s.drive != nil && directoryID != "" {
		sess, err := s.drive.Open(ctx)
		if err == nil {
			if err := sess.Upload(ctx, directoryID, subUploadName, []byte(vttContent)); err == nil {
				entries, _ := drive.DirectoryEntries(ctx, sess, directoryID)
				for _, entry := range entries {
					if entry.Name == subUploadName {
						fileID = entry.ID
						pickCode = entry.PickCode
						break
					}
				}
			}
		}
	}

	var track domain.SubtitleTrack
	err = ent.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		record, err := s.saveTrackTx(ctx, tx, saveTrackInput{
			MovieID:     movieID,
			FileID:      fileID,
			PickCode:    pickCode,
			Name:        subUploadName,
			DisplayName: candidate.DisplayName,
			Language:    candidate.Language,
			VersionTag:  candidate.Version,
			Source:      candidate.Source,
			SourceURL:   candidate.URL,
			StoragePath: filePath,
			IsDefault:   true,
		})
		if err != nil {
			return err
		}
		track = ToTrack(record)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &track, nil
}

// AutoFetchAndUpload finds the highest-scoring subtitle online, uploads it to 115 if possible, caches it locally, and stores it in the database.
func (s *Service) AutoFetchAndUpload(ctx context.Context, sess drive.Session, directoryID string, movieID int, code string, isUncensored bool) error {
	hasSub, err := s.db.Subtitle.Query().Where(subtitle.MovieIDEQ(movieID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check existing subtitles: %w", err)
	}
	if hasSub {
		return nil
	}

	candidates, err := s.aggregator.Search(ctx, code, isUncensored)
	if err != nil || len(candidates) == 0 {
		return nil
	}

	best := candidates[0]
	vttContent, err := s.aggregator.DownloadAndConvert(ctx, best)
	if err != nil {
		return fmt.Errorf("download best subtitle %s: %w", best.Name, err)
	}

	filePath, err := s.cacheVTT(movieID, vttContent)
	if err != nil {
		return fmt.Errorf("save subtitle cache: %w", err)
	}

	subUploadName := fmt.Sprintf("%s.%s.vtt", code, best.Language)
	if best.Version != VersionStandard {
		subUploadName = fmt.Sprintf("%s.%s.%s.vtt", code, best.Version, best.Language)
	}

	if s.embyDir != "" {
		prefix := codeid.Prefix(code)
		destDir := filepath.Join(s.embyDir, prefix, code)
		if err := os.MkdirAll(destDir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(destDir, subUploadName), []byte(vttContent), 0o644)
		}
	}

	var fileID string
	var pickCode string
	if sess != nil && directoryID != "" {
		if err := sess.Upload(ctx, directoryID, subUploadName, []byte(vttContent)); err == nil {
			// Find the uploaded file entry to acquire its pick code if available
			entries, _ := drive.DirectoryEntries(ctx, sess, directoryID)
			for _, entry := range entries {
				if entry.Name == subUploadName {
					fileID = entry.ID
					pickCode = entry.PickCode
					break
				}
			}
		}
	}

	return ent.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		_, err := s.saveTrackTx(ctx, tx, saveTrackInput{
			MovieID:     movieID,
			FileID:      fileID,
			PickCode:    pickCode,
			Name:        subUploadName,
			DisplayName: best.DisplayName,
			Language:    string(best.Language),
			VersionTag:  string(best.Version),
			Source:      best.Provider,
			SourceURL:   best.URL,
			StoragePath: filePath,
			IsDefault:   true,
		})
		return err
	})
}

// IndexLocalSubtitleTx records an existing subtitle file discovered on 115 during scanning within a transaction.
func IndexLocalSubtitleTx(ctx context.Context, tx *ent.Tx, movieID int, file pan.File) error {
	exists, err := tx.Subtitle.Query().
		Where(subtitle.MovieIDEQ(movieID), subtitle.FileIDEQ(file.ID)).
		Exist(ctx)
	if err != nil || exists {
		return err
	}

	ext := strings.ToLower(strings.TrimPrefix(path.Ext(file.Name), "."))
	lang := DetectChineseLanguage(file.Name, "")
	ver := DetectVersion(file.Name)
	displayName := BuildDisplayName(lang, ver, true)

	hasDefault, err := tx.Subtitle.Query().
		Where(subtitle.MovieIDEQ(movieID), subtitle.IsDefault(true)).
		Exist(ctx)
	if err != nil {
		return err
	}

	return tx.Subtitle.Create().
		SetMovieID(movieID).
		SetFileID(file.ID).
		SetPickCode(file.PickCode).
		SetName(file.Name).
		SetDisplayName(displayName).
		SetLanguage(string(lang)).
		SetFormat(ext).
		SetVersionTag(string(ver)).
		SetSource("local").
		SetOffsetMs(0).
		SetIsDefault(!hasDefault).
		Exec(ctx)
}

// IndexLocalSubtitle records an existing subtitle file discovered on 115 during scanning.
func (s *Service) IndexLocalSubtitle(ctx context.Context, movieID int, file pan.File) error {
	return ent.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		return IndexLocalSubtitleTx(ctx, tx, movieID, file)
	})
}

// GetTrackVTT retrieves WebVTT content for a given subtitle ID, applying any configured time offset
// or an optional query-level offsetOverride.
func (s *Service) GetTrackVTT(ctx context.Context, id int, offsetOverride *int) ([]byte, error) {
	record, err := s.db.Subtitle.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, domain.E(domain.KindNotFound, "字幕不存在", err)
		}
		return nil, fmt.Errorf("read subtitle %d: %w", id, err)
	}

	var rawContent string
	if record.StoragePath != "" {
		if contentBytes, err := os.ReadFile(record.StoragePath); err == nil {
			rawContent = string(contentBytes)
		}
	}

	if rawContent == "" && record.PickCode != "" && s.drive != nil {
		sess, err := s.drive.Open(ctx)
		if err != nil {
			return nil, fmt.Errorf("open drive for subtitle: %w", err)
		}
		data, err := sess.Read(ctx, record.PickCode, 10<<20)
		if err != nil {
			return nil, fmt.Errorf("read subtitle from 115: %w", err)
		}
		utf8Text, err := DecodeToUTF8(data)
		if err != nil {
			return nil, fmt.Errorf("decode subtitle charset: %w", err)
		}
		ext := record.Format
		if ext == "" {
			ext = strings.ToLower(strings.TrimPrefix(path.Ext(record.Name), "."))
		}
		vtt, err := ConvertToWebVTT(utf8Text, ext)
		if err != nil {
			return nil, fmt.Errorf("convert subtitle to vtt: %w", err)
		}
		rawContent = vtt

		// Cache to disk
		if filePath, err := s.cacheVTT(record.MovieID, vtt); err == nil {
			_ = s.db.Subtitle.UpdateOneID(record.ID).SetStoragePath(filePath).Exec(ctx)
		}
	}

	if rawContent == "" {
		return nil, domain.E(domain.KindNotFound, "字幕内容不可用", nil)
	}

	effectiveOffset := record.OffsetMs
	if offsetOverride != nil {
		effectiveOffset = *offsetOverride
	}

	if effectiveOffset != 0 {
		rawContent = ApplyTimeOffset(rawContent, effectiveOffset)
	}

	return []byte(rawContent), nil
}

// UpdateOffset updates the time offset (in milliseconds) for a subtitle track.
func (s *Service) UpdateOffset(ctx context.Context, id int, offsetMs int) error {
	return s.db.Subtitle.UpdateOneID(id).SetOffsetMs(offsetMs).Exec(ctx)
}

// SetDefault sets a specific subtitle as default for its movie, unsetting all other subtitles for that movie.
func (s *Service) SetDefault(ctx context.Context, movieID int, subID int) error {
	return ent.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		if err := tx.Subtitle.Update().
			Where(subtitle.MovieIDEQ(movieID)).
			SetIsDefault(false).
			Exec(ctx); err != nil {
			return err
		}
		return tx.Subtitle.UpdateOneID(subID).SetIsDefault(true).Exec(ctx)
	})
}

// Delete removes a subtitle record and its local cache file only when no other records share the same file.
func (s *Service) Delete(ctx context.Context, id int) error {
	record, err := s.db.Subtitle.Get(ctx, id)
	if err != nil {
		return err
	}
	if record.StoragePath != "" {
		// Only remove local file if no other subtitle record references this path
		otherCount, err := s.db.Subtitle.Query().
			Where(subtitle.StoragePathEQ(record.StoragePath), subtitle.IDNEQ(id)).
			Count(ctx)
		if err == nil && otherCount == 0 {
			_ = os.Remove(record.StoragePath)
		}
	}
	return s.db.Subtitle.DeleteOneID(id).Exec(ctx)
}
