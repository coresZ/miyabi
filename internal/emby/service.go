package emby

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/actor"
	"github.com/ppxb/miyabi/internal/gfriends"
)

// ServerInfo holds basic Emby instance details.
type ServerInfo struct {
	ServerName string `json:"server_name"`
	Version    string `json:"version"`
	ID         string `json:"id"`
}

// Service manages communication, configuration persistence, and batch notification to Emby.
type Service struct {
	db             *ent.Client
	mu             sync.RWMutex
	cfg            Config
	client         *http.Client
	queue          chan string
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	gfriends       *gfriends.Client
	actorSyncTimer *time.Timer
	actorSyncMu    sync.Mutex
}

// NewService instantiates an Emby service, restoring config from database or using defaults.
func NewService(ctx context.Context, db *ent.Client, initial Config) (*Service, error) {
	loaded, found, err := database.LoadSetting[Config](ctx, db, SettingKey)
	if err != nil {
		return nil, fmt.Errorf("load emby setting: %w", err)
	}

	cfg := initial
	if found {
		cfg = loaded
		if cfg.LocalDir == "" {
			cfg.LocalDir = initial.LocalDir
		}
	}

	subCtx, cancel := context.WithCancel(context.Background())
	s := &Service{
		db:  db,
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		queue:  make(chan string, 1000),
		ctx:    subCtx,
		cancel: cancel,
	}

	s.wg.Add(1)
	go s.worker(subCtx)

	if s.cfg.Enabled && s.cfg.IsSyncActors() {
		s.ScheduleActorSync(10 * time.Second)
	}

	return s, nil
}

// Close flushes the pending queue and stops background workers.
func (s *Service) Close() {
	s.actorSyncMu.Lock()
	if s.actorSyncTimer != nil {
		s.actorSyncTimer.Stop()
	}
	s.actorSyncMu.Unlock()

	s.cancel()
	s.wg.Wait()
}

// SetGFriends configures the GFriends client for actor avatar resolution.
func (s *Service) SetGFriends(g *gfriends.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gfriends = g
}

// ScheduleActorSync schedules an actor avatar sync run after the given delay.
func (s *Service) ScheduleActorSync(delay time.Duration) {
	s.actorSyncMu.Lock()
	defer s.actorSyncMu.Unlock()

	if s.actorSyncTimer != nil {
		s.actorSyncTimer.Stop()
	}
	s.actorSyncTimer = time.AfterFunc(delay, func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Minute)
		defer cancel()
		if _, err := s.SyncActorAvatars(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.WarnContext(ctx, "emby actor avatar sync finished with error", "error", err)
		}
	})
}

// Config returns the current active configuration.
func (s *Service) Config(context.Context) (Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg, nil
}

// UpdateConfig validates and persists the new configuration to the database.
func (s *Service) UpdateConfig(ctx context.Context, cfg Config) error {
	s.mu.Lock()
	if cfg.LocalDir == "" {
		cfg.LocalDir = s.cfg.LocalDir
	}
	s.mu.Unlock()

	if err := cfg.Normalize(); err != nil {
		return err
	}

	if err := database.SaveSetting(ctx, s.db, SettingKey, cfg); err != nil {
		return fmt.Errorf("save emby setting: %w", err)
	}

	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()

	if cfg.Enabled && cfg.IsSyncActors() {
		s.ScheduleActorSync(2 * time.Second)
	}

	return nil
}

// Test validates connection parameters by querying /System/Info.
func (s *Service) Test(ctx context.Context, cfg Config) (ServerInfo, error) {
	serverURL := strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	apiKey := strings.TrimSpace(cfg.APIKey)
	if serverURL == "" || apiKey == "" {
		return ServerInfo{}, domain.E(domain.KindInvalid, "请先填写 Emby 服务器地址与 API Key", nil)
	}
	return s.Ping(ctx, serverURL, apiKey)
}

// Ping checks server connectivity and returns instance info.
func (s *Service) Ping(ctx context.Context, serverURL, apiKey string) (ServerInfo, error) {
	serverURL = strings.TrimRight(serverURL, "/")
	reqURL := fmt.Sprintf("%s/System/Info", serverURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return ServerInfo{}, domain.E(domain.KindInvalid, "创建请求失败", err)
	}
	req.Header.Set("X-Emby-Token", apiKey)
	q := req.URL.Query()
	q.Set("api_key", apiKey)
	req.URL.RawQuery = q.Encode()

	resp, err := s.client.Do(req)
	if err != nil {
		return ServerInfo{}, domain.E(domain.KindUpstream, fmt.Sprintf("无法连接到 Emby 服务器: %v", err), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ServerInfo{}, domain.E(domain.KindUnauthorized, "Emby API Key 无效或权限不足", nil)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ServerInfo{}, domain.E(domain.KindUpstream, fmt.Sprintf("Emby 服务器返回错误 (HTTP %d): %s", resp.StatusCode, string(body)), nil)
	}

	var raw struct {
		ServerName string `json:"ServerName"`
		Version    string `json:"Version"`
		ID         string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return ServerInfo{}, domain.E(domain.KindUpstream, "解析 Emby 响应失败", err)
	}

	return ServerInfo{
		ServerName: raw.ServerName,
		Version:    raw.Version,
		ID:         raw.ID,
	}, nil
}

// NotifyUpdated enqueues a directory to be batched and notified to Emby.
func (s *Service) NotifyUpdated(localPath string) {
	s.mu.RLock()
	enabled := s.cfg.Enabled && s.cfg.ServerURL != "" && s.cfg.APIKey != ""
	s.mu.RUnlock()

	if !enabled || strings.TrimSpace(localPath) == "" {
		return
	}

	select {
	case s.queue <- localPath:
	default:
		slog.Warn("emby notification queue full, dropping path", "path", localPath)
	}
}

type mediaUpdateItem struct {
	Path       string `json:"Path"`
	UpdateType string `json:"UpdateType"`
}

type mediaUpdateRequest struct {
	Updates []mediaUpdateItem `json:"Updates"`
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	pending := make(map[string]bool)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		clear(pending)

		if err := s.sendBatch(ctx, paths); err != nil {
			slog.WarnContext(ctx, "failed to notify emby of updated media", "count", len(paths), "error", err)
		} else {
			slog.InfoContext(ctx, "notified emby of updated media", "count", len(paths))
			s.ScheduleActorSync(25 * time.Second)
		}
	}

	for {
		select {
		case <-ctx.Done():
			if len(pending) > 0 {
				paths := make([]string, 0, len(pending))
				for p := range pending {
					paths = append(paths, p)
				}
				clear(pending)

				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = s.sendBatch(shutdownCtx, paths)
				shutdownCancel()
			}
			return
		case item := <-s.queue:
			pending[item] = true
			if len(pending) >= 50 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (s *Service) sendBatch(ctx context.Context, localPaths []string) error {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	if !cfg.Enabled || cfg.ServerURL == "" || cfg.APIKey == "" || len(localPaths) == 0 {
		return nil
	}

	updates := make([]mediaUpdateItem, 0, len(localPaths))
	for _, lp := range localPaths {
		embyPath := s.translatePath(lp, cfg.LocalDir, cfg.MediaPath)
		if embyPath != "" {
			updates = append(updates, mediaUpdateItem{
				Path:       embyPath,
				UpdateType: "Created",
			})
		}
	}

	if len(updates) == 0 {
		return nil
	}

	payload, err := json.Marshal(mediaUpdateRequest{Updates: updates})
	if err != nil {
		return err
	}

	reqURL := fmt.Sprintf("%s/Library/Media/Updated", strings.TrimRight(cfg.ServerURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Token", cfg.APIKey)
	q := req.URL.Query()
	q.Set("api_key", cfg.APIKey)
	req.URL.RawQuery = q.Encode()

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("emby returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (s *Service) translatePath(localPath, localDir, mediaPath string) string {
	localPath = filepath.Clean(localPath)
	if mediaPath == "" {
		return filepath.ToSlash(localPath)
	}

	mediaPath = strings.TrimRight(filepath.ToSlash(mediaPath), "/")
	if localDir != "" {
		cleanLocalDir := filepath.Clean(localDir)
		rel, err := filepath.Rel(cleanLocalDir, localPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			slashRel := filepath.ToSlash(rel)
			return path.Join(mediaPath, slashRel)
		}
	}

	return path.Join(mediaPath, filepath.ToSlash(filepath.Base(localPath)))
}

// PersonItem represents an Emby person/actor entry.
type PersonItem struct {
	Name            string            `json:"Name"`
	ID              string            `json:"Id"`
	PrimaryImageTag string            `json:"PrimaryImageTag,omitempty"`
	ImageTags       map[string]string `json:"ImageTags,omitempty"`
}

// ListPersonsWithoutAvatar queries Emby for persons missing a primary avatar image.
func (s *Service) ListPersonsWithoutAvatar(ctx context.Context) ([]PersonItem, error) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	if !cfg.Enabled || cfg.ServerURL == "" || cfg.APIKey == "" {
		return nil, nil
	}

	reqURL := fmt.Sprintf("%s/Persons?api_key=%s", strings.TrimRight(cfg.ServerURL, "/"), url.QueryEscape(cfg.APIKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Emby-Token", cfg.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("emby /Persons returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Items []PersonItem `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var missing []PersonItem
	for _, item := range result.Items {
		if item.ID == "" || strings.TrimSpace(item.Name) == "" {
			continue
		}
		hasImage := item.PrimaryImageTag != "" || (item.ImageTags != nil && item.ImageTags["Primary"] != "")
		if !hasImage {
			missing = append(missing, item)
		}
	}
	return missing, nil
}

// UploadPersonAvatar uploads an avatar image to an Emby person.
func (s *Service) UploadPersonAvatar(ctx context.Context, personID string, imageBytes []byte) error {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	if !cfg.Enabled || cfg.ServerURL == "" || cfg.APIKey == "" || personID == "" || len(imageBytes) == 0 {
		return nil
	}

	reqURL := fmt.Sprintf("%s/Items/%s/Images/Primary?api_key=%s",
		strings.TrimRight(cfg.ServerURL, "/"), url.PathEscape(personID), url.QueryEscape(cfg.APIKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(imageBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("X-Emby-Token", cfg.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("upload avatar returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// SyncActorAvatars scans Emby for persons without avatars and uploads matching GFriends or JavDB avatars.
func (s *Service) SyncActorAvatars(ctx context.Context) (int, error) {
	s.mu.RLock()
	cfg := s.cfg
	g := s.gfriends
	s.mu.RUnlock()

	if !cfg.Enabled || !cfg.IsSyncActors() || cfg.ServerURL == "" || cfg.APIKey == "" {
		return 0, nil
	}

	missing, err := s.ListPersonsWithoutAvatar(ctx)
	if err != nil {
		return 0, fmt.Errorf("list persons without avatar: %w", err)
	}

	if len(missing) == 0 {
		return 0, nil
	}

	slog.InfoContext(ctx, "emby actor avatar sync started", "missing_count", len(missing))
	uploaded := 0

	for _, person := range missing {
		if err := ctx.Err(); err != nil {
			return uploaded, err
		}

		var avatarBytes []byte
		// 1. Try GFriends
		if g != nil {
			data, err := g.FetchAvatar(ctx, person.Name)
			if err == nil && len(data) > 0 {
				avatarBytes = data
			}
		}

		// 2. Fallback to local DB Actor avatar (from JavDB)
		if len(avatarBytes) == 0 && s.db != nil {
			act, err := s.db.Actor.Query().Where(actor.NameEQ(person.Name)).First(ctx)
			if err == nil && act != nil && act.Avatar != nil && *act.Avatar != "" {
				avatarBytes, _ = s.downloadImage(ctx, *act.Avatar)
			}
		}

		// 3. Upload if found
		if len(avatarBytes) > 0 {
			if err := s.UploadPersonAvatar(ctx, person.ID, avatarBytes); err != nil {
				slog.WarnContext(ctx, "failed to upload avatar for actor", "name", person.Name, "error", err)
			} else {
				uploaded++
				slog.DebugContext(ctx, "uploaded actor avatar to emby", "name", person.Name)
			}
		}

		// Rate limiting: sleep 200ms
		select {
		case <-ctx.Done():
			return uploaded, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	slog.InfoContext(ctx, "emby actor avatar sync completed", "uploaded", uploaded, "total_missing", len(missing))
	return uploaded, nil
}

func (s *Service) downloadImage(ctx context.Context, imgURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download status: %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}
