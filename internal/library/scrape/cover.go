package scrape

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

// CoverPayload describes the input and intermediate state of a cover creation job.
type CoverPayload struct {
	MetadataPayload
	ScrapeTaskID int                 `json:"scrape_task_id"`
	Document     nfo.Movie           `json:"document"`
	CoverURL     string              `json:"cover_url,omitempty"`
	Origin       *ArtworkOrigin      `json:"origin,omitempty"`
	Artwork      *mediaimage.Artwork `json:"artwork,omitempty"`
	Snapshot     *Snapshot           `json:"snapshot,omitempty"`
}

// Cover processes the cover download, generation, and upload of artwork and NFO sidecars.
func (service *Service) Cover(ctx context.Context, job tasks.Job) error {
	input, err := tasks.DecodePayload[CoverPayload](job.Payload)
	if err != nil {
		return err
	}
	if input.Snapshot != nil {
		return nil
	}
	if err := service.artwork.Lock(ctx); err != nil {
		return err
	}
	defer service.artwork.Unlock()
	return service.processCover(ctx, job, input)
}

func (service *Service) processCover(ctx context.Context, job tasks.Job, input CoverPayload) error {
	input.Code = codeid.Normalize(input.Code)
	input.Document.Code = codeid.Normalize(input.Document.Code)
	sess, err := service.begin(ctx, input.MetadataPayload)
	if err != nil {
		return err
	}
	var artwork mediaimage.Artwork
	switch {
	case input.Artwork != nil:
		artwork = *input.Artwork
	case input.Origin != nil:
		poster, err := service.originImage(ctx, sess, input.Origin.Poster)
		if err != nil {
			return err
		}
		fanart, err := service.originImage(ctx, sess, input.Origin.Fanart)
		if err != nil {
			return err
		}
		artwork, err = service.images.Restore(poster, fanart)
		if err != nil {
			return err
		}
	default:
		if input.CoverURL == "" {
			return domain.E(domain.KindNotFound, "JavDB 未返回影片封面", nil)
		}
		media, err := service.discover.Media(ctx, input.CoverURL)
		if err != nil {
			return err
		}
		artwork, err = service.images.FromCover(media.Body)
		if err != nil {
			return err
		}
	}
	poster, err := service.images.ReadURL(artwork.Poster)
	if err != nil {
		return fmt.Errorf("read cached poster: %w", err)
	}
	fanart, err := service.images.ReadURL(artwork.Fanart)
	if err != nil {
		return fmt.Errorf("read cached fanart: %w", err)
	}
	input.Artwork = &artwork
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		return err
	}
	if err := service.db.Task.UpdateOneID(job.ID).SetPayload(encoded).Exec(ctx); err != nil {
		return err
	}
	// Re-read the directory after scraping. Never upload alongside a video
	// which was deleted or moved while waiting for JavDB or another task.
	directories, err := service.directories(ctx, sess, input.MetadataPayload)
	if err != nil {
		return err
	}
	snapshot := &Snapshot{}
	var videos []pan.File
	for i, directory := range directories {
		state, err := service.writeSidecars(ctx, sess, input, directory, poster, fanart)
		if err != nil {
			return err
		}
		snapshot.Directories = append(snapshot.Directories, state)
		for _, entry := range directory.Files {
			if directory.VideoIDs[entry.ID] {
				videos = append(videos, entry)
			}
		}
		if err := service.db.Task.UpdateOneID(job.ID).SetProgress((i + 1) * 100 / len(directories)).Exec(ctx); err != nil {
			return err
		}
	}
	snapshot.Videos = VideoFingerprint(videos)
	input.Snapshot = snapshot
	encoded, err = tasks.EncodePayload(input)
	if err != nil {
		return err
	}
	if err := sess.Commit(ctx, func(tx *ent.Tx) error {
		if err := tx.Movie.UpdateOneID(input.MovieID).SetCode(input.Code).
			SetCover(artwork.Thumbnail).SetPoster(artwork.Poster).SetFanarts([]string{artwork.Fanart}).
			SetScrapeStatus(movie.ScrapeStatusDone).Exec(ctx); err != nil {
			return err
		}
		return tx.Task.UpdateOneID(job.ID).SetPayload(encoded).Exec(ctx)
	}); err != nil {
		return fmt.Errorf("save movie artwork: %w", err)
	}

	if service.subtitles != nil && len(directories) > 0 {
		isUncensored := false
		for _, v := range videos {
			low := strings.ToLower(v.Name)
			if strings.Contains(low, "uncensored") || strings.Contains(v.Name, "无码") {
				isUncensored = true
				break
			}
		}
		_ = service.subtitles.AutoFetchAndUpload(ctx, sess, directories[0].ID, input.MovieID, input.Code, isUncensored)
	}

	if service.notifier != nil {
		service.notifier.NotifyLibraryChanged()
	}
	return nil
}

func (service *Service) originImage(ctx context.Context, sess drive.Session, entry pan.File) ([]byte, error) {
	info, err := drive.SourceInfo(ctx, sess, entry.ID)
	if err != nil {
		return nil, fmt.Errorf("find NFO artwork: %w", err)
	}
	return sess.Read(ctx, info.File.PickCode, 32<<20)
}

func (service *Service) writeSidecars(ctx context.Context, sess drive.Session, input CoverPayload, directory MovieDirectory, poster, fanart []byte) (DirectorySnapshot, error) {
	var snapshot DirectorySnapshot
	stem := nfo.FileStem(input.Code)
	nfoName := stem + ".nfo"
	// An existing matching NFO is already the source of truth. Preserve its
	// formatting and user edits, as well as its referenced artwork.
	if doc, origin, found, err := DirectoryNFO(ctx, sess, input.Code, directory); err != nil {
		return snapshot, err
	} else if found {
		if err := VerifyCoverOrigin(input, directory.ID, doc, *origin, poster, fanart); err != nil {
			return snapshot, err
		}
		return NewDirectorySnapshot(directory.ID, origin.NFO, origin.Poster, origin.Fanart), nil
	}
	posterName, fanartName := "poster.jpg", "fanart.jpg"
	existingPoster, posterExists := SidecarByName(directory.Files, posterName)
	existingFanart, fanartExists := SidecarByName(directory.Files, fanartName)
	if directory.Shared || directory.ID == input.Source.Directory.ID || (posterExists && !strings.EqualFold(existingPoster.SHA1, pan.SHA1(poster))) ||
		(fanartExists && !strings.EqualFold(existingFanart.SHA1, pan.SHA1(fanart))) {
		posterName, fanartName = stem+"-poster.jpg", stem+"-fanart.jpg"
	}
	doc := input.Document
	doc.Thumbs = []nfo.Thumb{{Aspect: "poster", Path: posterName}}
	doc.Fanart = fanartName
	body, err := nfo.Encode(doc)
	if err != nil {
		return snapshot, err
	}
	// NFO is the completion marker and is written last. A restarted job can
	// reuse previously uploaded images without creating same-name duplicates.
	for _, item := range []struct {
		name string
		body []byte
	}{
		{posterName, poster}, {fanartName, fanart}, {nfoName, body},
	} {
		if existing, found := SidecarByName(directory.Files, item.name); found {
			if strings.EqualFold(existing.SHA1, pan.SHA1(item.body)) {
				continue
			}
			return snapshot, domain.E(domain.KindConflict, fmt.Sprintf("媒体目录已存在不同内容的 %s，已保留原文件", item.name), nil)
		}
		if err := UploadSidecar(ctx, sess, directory, item.name, item.body); err != nil {
			return snapshot, fmt.Errorf("write %s to 115: %w", item.name, err)
		}
	}
	return NewDirectorySnapshot(directory.ID, pan.File{Name: nfoName, SHA1: pan.SHA1(body)},
		pan.File{Name: posterName, SHA1: pan.SHA1(poster)}, pan.File{Name: fanartName, SHA1: pan.SHA1(fanart)}), nil
}

// VerifyCoverOrigin validates that existing sidecars have not changed concurrently.
func VerifyCoverOrigin(input CoverPayload, directoryID string, current nfo.Movie, origin ArtworkOrigin, poster, fanart []byte) error {
	expected := input.Document
	posterSHA, fanartSHA := pan.SHA1(poster), pan.SHA1(fanart)
	// directories() orders parent IDs as text; later NFOs keep their own edits.
	if input.Origin != nil && directoryID > input.Origin.Poster.ParentID {
		return nil
	}
	if input.Origin != nil && directoryID == input.Origin.Poster.ParentID {
		posterSHA, fanartSHA = input.Origin.Poster.SHA1, input.Origin.Fanart.SHA1
	} else {
		expected.Thumbs = []nfo.Thumb{{Aspect: "poster", Path: origin.Poster.Name}}
		expected.Fanart = origin.Fanart.Name
	}
	before, err := nfo.Encode(expected)
	if err != nil {
		return err
	}
	after, err := nfo.Encode(current)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) || !strings.EqualFold(posterSHA, origin.Poster.SHA1) || !strings.EqualFold(fanartSHA, origin.Fanart.SHA1) {
		return domain.E(domain.KindConflict, "NFO 或图片在处理期间发生变化，请重新扫描", nil)
	}
	return nil
}

// UploadSidecar writes a sidecar file into a movie's directory on 115 storage.
func UploadSidecar(ctx context.Context, sess drive.Session, directory MovieDirectory, name string, body []byte) error {
	for _, entry := range directory.Files {
		if !directory.VideoIDs[entry.ID] {
			continue
		}
		info, err := sess.Info(ctx, entry.ID)
		if err != nil {
			return err
		}
		if info.ParentID != directory.ID || !drive.WithinSource(info, sess.Source()) {
			return domain.E(domain.KindConflict, "视频已移动，请重新扫描", nil)
		}
		return sess.Upload(ctx, directory.ID, name, body)
	}
	return domain.E(domain.KindInvalid, "没有可写入元数据的视频目录", nil)
}
