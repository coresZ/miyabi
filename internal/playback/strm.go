package playback

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/pan"
)

// StreamURL resolves the direct playback URL from 115 for a given fileID.
// It queries the 115 pick code from the database (or 115 session info) and
// requests playback sources, returning the best stream (prefers definition 100 / highest height).
func (service *Service) StreamURL(ctx context.Context, fileID string) (string, error) {
	if service.drive == nil {
		return "", drive.ErrMediaDirectoryRequired
	}
	sess, err := service.drive.Open(ctx)
	if err != nil {
		return "", err
	}

	var pickCode string
	record, err := service.database.File.Query().
		Where(file.FileIDEQ(fileID)).
		Select(file.FieldPickCode).
		First(ctx)
	if err == nil && record != nil && record.PickCode != "" {
		pickCode = record.PickCode
	}

	if pickCode == "" {
		info, err := sess.Info(ctx, fileID)
		if err != nil {
			return "", fmt.Errorf("read 115 video info: %w", err)
		}
		pickCode = info.PickCode
	}

	if pickCode == "" {
		return "", domain.E(domain.KindNotFound, "未能获取到视频的 115 提取码", nil)
	}

	sources, err := sess.PlayURL(ctx, pickCode)
	if err != nil {
		if errors.Is(err, pan.ErrTranscodeUnavailable) {
			return "", drive.ErrTranscodeUnavailable
		}
		return "", fmt.Errorf("get 115 playback URL: %w", err)
	}

	var best string
	var maxScore int
	for _, src := range sources {
		if src.URL == "" {
			continue
		}
		score := src.Height
		if src.Definition == 100 {
			score += 100000
		}
		if score > maxScore || best == "" {
			maxScore = score
			best = src.URL
		}
	}
	if best == "" {
		return "", domain.E(domain.KindNotFound, "115 未返回有效视频播放流", nil)
	}
	return best, nil
}

// OpenMedia forwards media requests (e.g. HEAD probes) to 115 CDN.
func (service *Service) OpenMedia(ctx context.Context, method, address string, headers http.Header) (*http.Response, error) {
	if service.drive == nil {
		return nil, drive.ErrMediaDirectoryRequired
	}
	return service.drive.OpenMedia(ctx, method, address, headers)
}
