package service

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/predicate"
	"github.com/ppxb/miyabi/internal/ent/watchhistory"
	"github.com/ppxb/miyabi/internal/library"
	"github.com/ppxb/miyabi/internal/library/scan"
	"github.com/ppxb/miyabi/internal/pan"
)

type LibraryFile = library.File
type WatchHistoryScope = library.WatchHistoryScope
type WatchResume = library.WatchResume

func historyScope(source domain.LibrarySource) predicate.WatchHistory {
	return watchhistory.And(watchhistory.AccountIDEQ(source.AccountID), watchhistory.RootIDEQ(source.Directory.ID))
}

type PlayFiles struct {
	Code   string            `json:"code"`
	Title  string            `json:"title"`
	Files  []LibraryFile     `json:"files"`
	Source WatchHistoryScope `json:"source"`
	Resume *WatchResume      `json:"resume,omitempty"`
}

type MediaSource struct {
	Src   string `json:"src"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

type Playback struct {
	ID      string        `json:"id"`
	Sources []MediaSource `json:"sources"`
}

type playResource struct {
	url      *url.URL
	playlist bool
}

type playSession struct {
	id        string
	source    domain.LibrarySource
	version   uint64
	ctx       context.Context
	cancel    context.CancelFunc
	timer     *time.Timer
	resources []playResource
	byURL     map[string]int
}

type PlayService struct {
	drive    *drive.Drive
	library  *library.Service
	mu       sync.Mutex
	sessions map[string]*playSession
}

func NewPlayService(library *library.Service, d *drive.Drive) *PlayService {
	return &PlayService{library: library, drive: d, sessions: make(map[string]*playSession)}
}

func (service *PlayService) Files(ctx context.Context, movieID int) (PlayFiles, error) {
	source := service.drive.Source()
	if source == nil {
		return PlayFiles{}, drive.ErrMediaDirectoryRequired
	}
	scope := scan.LibraryFiles(*source)
	record, err := service.library.Database().Movie.Query().
		Where(movie.IDEQ(movieID), movie.HasFilesWith(scope)).
		WithFiles(func(query *ent.FileQuery) {
			query.Where(scope).Order(ent.Desc(file.FieldSize), ent.Asc(file.FieldPath), ent.Asc(file.FieldID))
		}).Only(ctx)
	if err != nil {
		return PlayFiles{}, fmt.Errorf("read playable movie: %w", err)
	}
	result := PlayFiles{Code: record.Code, Title: record.Title, Files: make([]LibraryFile, 0, len(record.Edges.Files))}
	for _, entry := range record.Edges.Files {
		result.Files = append(result.Files, LibraryFile{ID: entry.FileID, Name: entry.Name, Path: entry.Path, Size: entry.Size})
	}
	result.Source = WatchHistoryScope{AccountID: source.AccountID, DirectoryID: source.Directory.ID}
	history, err := service.library.Database().WatchHistory.Query().
		Where(historyScope(*source), watchhistory.MovieIDEQ(movieID)).
		Select(watchhistory.FieldID, watchhistory.FieldFileID, watchhistory.FieldPosition, watchhistory.FieldDuration).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return PlayFiles{}, fmt.Errorf("read playback resume: %w", err)
	}
	if history != nil {
		result.Resume = &WatchResume{ID: history.ID, FileID: history.FileID, Position: history.Position, Duration: history.Duration}
	}
	return result, nil
}

func (service *PlayService) Start(ctx context.Context, fileID string) (Playback, error) {
	sess, err := service.drive.Open(ctx)
	if err != nil {
		return Playback{}, err
	}
	source := sess.Source()
	_, err = service.library.Database().File.Query().Where(scan.LibraryFiles(source), file.FileIDEQ(fileID)).Only(ctx)
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
		return Playback{}, fmt.Errorf("get 115 playback URL: %w", err)
	}
	return service.createSession(source, sess.Version(), sources)
}

func (service *PlayService) createSession(source domain.LibrarySource, version uint64, sources []pan.PlaySource) (Playback, error) {
	session := &playSession{id: uuid.NewString(), source: source, version: version, byURL: make(map[string]int)}
	result := Playback{ID: session.id, Sources: make([]MediaSource, 0, len(sources))}
	for _, source := range sources {
		address, err := url.Parse(source.URL)
		if err != nil {
			return Playback{}, fmt.Errorf("115 returned an invalid playback URL")
		}
		local, err := session.register(address, true)
		if err != nil {
			return Playback{}, err
		}
		item := MediaSource{Src: local, Type: "application/x-mpegurl", Label: fmt.Sprintf("%dp", source.Height)}
		if source.Definition == 100 {
			item.Label = "原画 · " + item.Label
		}
		result.Sources = append(result.Sources, item)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	session.ctx, session.cancel = context.WithCancel(context.Background())
	service.sessions[session.id] = session
	// Explicit close releases immediately; this also clears sessions abandoned by a closed tab.
	session.timer = time.AfterFunc(8*time.Hour, func() { service.Release(session.id) })
	return result, nil
}

// Only URLs returned by 115 or referenced by its playlists become proxy resources.
// The browser receives opaque local paths, never signed upstream URLs.
func (session *playSession) register(address *url.URL, playlist bool) (string, error) {
	if (address.Scheme != "https" && address.Scheme != "http") || address.Host == "" || address.User != nil {
		return "", fmt.Errorf("115 returned an unsupported media URL")
	}
	key := address.String()
	index, exists := session.byURL[key]
	if !exists {
		index = len(session.resources)
		session.byURL[key] = index
		session.resources = append(session.resources, playResource{url: address, playlist: playlist})
	} else if playlist {
		session.resources[index].playlist = true
	}
	return "/api/play/" + session.id + "/stream/" + strconv.Itoa(index), nil
}

func (service *PlayService) Release(id string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if session, exists := service.sessions[id]; exists {
		session.timer.Stop()
		session.cancel()
		delete(service.sessions, id)
	}
}

func (service *PlayService) Close() {
	service.mu.Lock()
	defer service.mu.Unlock()
	for id, session := range service.sessions {
		session.timer.Stop()
		session.cancel()
		delete(service.sessions, id)
	}
}

func (service *PlayService) resource(id string, index int) (*playSession, playResource, error) {
	service.mu.Lock()
	session, exists := service.sessions[id]
	if !exists || index < 0 || index >= len(session.resources) {
		service.mu.Unlock()
		return nil, playResource{}, domain.E(domain.KindNotFound, "播放会话已结束，请重新播放", fs.ErrNotExist)
	}
	resource := session.resources[index]
	service.mu.Unlock()

	if !service.drive.ValidateSource(session.source, session.version) {
		service.Release(id)
		return nil, playResource{}, domain.E(domain.KindNotFound, "登录账号或媒体目录已变更，请重新播放", fs.ErrNotExist)
	}
	return session, resource, nil
}
