package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
)

// LocalVideo is one local media file playable from the built-in player.
type LocalVideo struct {
	FileID string `json:"file_id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

// LocalVideos lists the raw video files of a movie that live on local storage.
// Exported .strm files are skipped: they point at remote streams, not files
// this server can read.
func (s *Service) LocalVideos(ctx context.Context, movieID int) ([]LocalVideo, error) {
	records, err := s.database.File.Query().Where(
		file.AccountIDEQ(domain.LocalAccountID), file.MovieIDEQ(movieID),
	).Order(ent.Asc(file.FieldName), ent.Asc(file.FieldID)).Select(
		file.FieldFileID, file.FieldName, file.FieldSize).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list local videos: %w", err)
	}
	videos := make([]LocalVideo, 0, len(records))
	for _, record := range records {
		if !domain.IsVideo(record.Name) {
			continue
		}
		videos = append(videos, LocalVideo{FileID: record.FileID, Name: record.Name, Size: record.Size})
	}
	return videos, nil
}

// LocalPlayFile opens an indexed local video, refusing any path that escapes
// the directory it was scanned from.
func (s *Service) LocalPlayFile(ctx context.Context, fileID string) (*os.File, string, error) {
	record, err := s.database.File.Query().Where(
		file.FileIDEQ(fileID), file.AccountIDEQ(domain.LocalAccountID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, "", domain.E(domain.KindNotFound, "本地视频不存在", nil)
	}
	if err != nil {
		return nil, "", err
	}
	if !domain.IsVideo(record.Name) {
		return nil, "", domain.E(domain.KindNotFound, "该文件不是本地视频", nil)
	}
	path, err := localFilePath(record.RootID, record.Path)
	if err != nil {
		return nil, "", err
	}
	opened, err := os.Open(path)
	if err != nil {
		return nil, "", domain.E(domain.KindNotFound, "本地视频文件不可访问", err)
	}
	return opened, record.Name, nil
}

// localFilePath resolves a file inside its scan root and rejects symlinks or
// relative segments that would escape that root.
func localFilePath(root, rel string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", domain.E(domain.KindInvalid, "本地视频缺少有效的目录信息", nil)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", domain.E(domain.KindNotFound, "本地媒体目录不可访问", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(canonicalRoot, rel))
	if err != nil {
		return "", domain.E(domain.KindNotFound, "本地视频文件不可访问", err)
	}
	within, err := filepath.Rel(canonicalRoot, resolved)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(os.PathSeparator)) || filepath.IsAbs(within) {
		return "", domain.E(domain.KindInvalid, "本地视频路径越界", nil)
	}
	return resolved, nil
}
