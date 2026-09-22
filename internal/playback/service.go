package playback

import (
	"sync"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
)

type PlayFiles struct {
	Code   string                   `json:"code"`
	Title  string                   `json:"title"`
	Files  []domain.LibraryFile     `json:"files"`
	Source domain.WatchHistoryScope `json:"source"`
	Resume *domain.WatchResume      `json:"resume,omitempty"`
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

// Service manages video playback sessions, file selection, and stream proxying.
type Service struct {
	database *ent.Client
	drive    *drive.Drive
	mu       sync.Mutex
	sessions map[string]*playSession
}

func New(database *ent.Client, d *drive.Drive) *Service {
	return &Service{
		database: database,
		drive:    d,
		sessions: make(map[string]*playSession),
	}
}

func (service *Service) Close() {
	service.mu.Lock()
	defer service.mu.Unlock()
	for id, session := range service.sessions {
		session.timer.Stop()
		session.cancel()
		delete(service.sessions, id)
	}
}
