package monitor

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/setting"
	"github.com/ppxb/miyabi/internal/ent/subscription"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/magnet"
	"github.com/ppxb/miyabi/internal/syncx"
	"github.com/ppxb/miyabi/internal/tasks"
)

const (
	monitorPreReleaseInterval = 72 * time.Hour
	monitorReleasedInterval   = 24 * time.Hour
	monitorRetryInterval      = time.Hour
	monitorStaleAfterDays     = 30
	monitorBatchSize          = 10
	monitorRequestGap         = 2 * time.Second

	subscriptionConfigSetting = "subscription.config"
)

type Status string

const (
	StatusWaiting Status = "waiting"
	StatusAdded   Status = "added"
	StatusStale   Status = "stale"
	StatusActive  Status = "active"
	StatusPaused  Status = "paused"
	StatusError   Status = "error"
)

type Config struct {
	MovieAutoDownload bool               `json:"movie_auto_download"`
	ActorAutoDownload bool               `json:"actor_auto_download"`
	ActorCheckTime    string             `json:"actor_check_time"`
	Preferences       magnet.Preferences `json:"preferences"`
}

func DefaultConfig() Config {
	return Config{
		MovieAutoDownload: true,
		ActorAutoDownload: false,
		ActorCheckTime:    "04:00",
		Preferences:       magnet.DefaultPreferences(),
	}
}

type Item struct {
	ID            int        `json:"id"`
	Kind          string     `json:"kind"`
	TargetID      string     `json:"target_id"`
	Code          string     `json:"code,omitempty"`
	Title         string     `json:"title"`
	Cover         string     `json:"cover"`
	ReleaseDate   string     `json:"release_date,omitempty"`
	OriginID      *int       `json:"origin_id,omitempty"`
	AutoDownload  bool       `json:"auto_download"`
	Zone          string     `json:"zone,omitempty"`
	Status        Status     `json:"status"`
	Cursor        string     `json:"cursor,omitempty"`
	Hash          string     `json:"hash,omitempty"`
	TaskID        *int       `json:"task_id,omitempty"`
	NextCheckAt   *time.Time `json:"next_check_at,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	Checks        int        `json:"checks"`
	Error         *string    `json:"error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type ActorCursor struct {
	LatestReleaseDate string   `json:"latest_release_date"`
	SeenMovieIDs      []string `json:"seen_movie_ids"`
}

type BatchPayload struct {
	IDs          []int `json:"ids"`
	Total        int   `json:"total"`
	Processed    int   `json:"processed"`
	SuccessCount int   `json:"success_count"`
	FailedCount  int   `json:"failed_count"`
}

type Discoverer interface {
	MovieSummary(ctx context.Context, movieID string) (domain.MovieSummary, error)
	CatalogueMagnets(ctx context.Context, movieID string) ([]domain.Magnet, error)
	BrowseMovies(ctx context.Context, options domain.BrowseOptions) ([]domain.Movie, error)
}

type OfflineAdder interface {
	Add(ctx context.Context, movieID string, hash string) (domain.OfflineSubmission, error)
}

// Service manages movie and actor subscriptions, automatic 115 ingestion,
// and batch queued tasks.
type Service struct {
	database *ent.Client
	discover Discoverer
	offline  OfflineAdder
	tasks    *tasks.Service
	checking syncx.ContextLock
	wake     chan struct{}
}

func New(database *ent.Client, discover Discoverer, offline OfflineAdder, taskSvc *tasks.Service) *Service {
	return &Service{
		database: database,
		discover: discover,
		offline:  offline,
		tasks:    taskSvc,
		wake:     make(chan struct{}, 1),
	}
}

// Pending signals when a subscription wants an immediate check.
func (service *Service) Pending() <-chan struct{} {
	return service.wake
}

func (service *Service) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

func (service *Service) Config(ctx context.Context) (Config, error) {
	cfg, found, err := loadSetting[Config](ctx, service.database, subscriptionConfigSetting)
	if err != nil {
		return DefaultConfig(), err
	}
	if !found {
		return DefaultConfig(), nil
	}
	return cfg, nil
}

func (service *Service) UpdateConfig(ctx context.Context, cfg Config) error {
	if cfg.ActorCheckTime == "" {
		cfg.ActorCheckTime = "04:00"
	}
	return saveSetting(ctx, service.database, subscriptionConfigSetting, cfg)
}

func (service *Service) List(ctx context.Context, kind string, page int, limit int) ([]Item, error) {
	query := service.database.Subscription.Query()
	if kind != "" {
		query = query.Where(subscription.KindEQ(subscription.Kind(kind)))
	}
	query = query.Order(ent.Desc(subscription.FieldID))
	if limit > 0 {
		if page < 1 {
			page = 1
		}
		query = query.Offset((page - 1) * limit).Limit(limit)
	}

	records, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	result := make([]Item, len(records))
	for index, record := range records {
		result[index] = subscriptionItem(record)
	}
	return result, nil
}

type AddMovieOptions struct {
	AutoDownload *bool
	Zone         string
	OriginID     *int
}

// AddMovie starts watching a movie or resets a stale/added record.
func (service *Service) AddMovie(ctx context.Context, movieID string, opts AddMovieOptions) (Item, error) {
	existing, err := service.database.Subscription.Query().
		Where(subscription.KindEQ(subscription.KindMovie), subscription.TargetIDEQ(movieID)).Only(ctx)
	if err == nil {
		if existing.Status == subscription.StatusWaiting {
			return subscriptionItem(existing), nil
		}
		return service.Retry(ctx, existing.ID)
	}
	if !ent.IsNotFound(err) {
		return Item{}, fmt.Errorf("find subscription: %w", err)
	}

	detail, err := service.discover.MovieSummary(ctx, movieID)
	if err != nil {
		return Item{}, err
	}

	cfg, _ := service.Config(ctx)
	autoDownload := cfg.MovieAutoDownload
	if opts.AutoDownload != nil {
		autoDownload = *opts.AutoDownload
	}

	now := time.Now()
	create := service.database.Subscription.Create().
		SetKind(subscription.KindMovie).
		SetTargetID(detail.ID).
		SetCode(detail.Code).
		SetTitle(detail.Title).
		SetCover(detail.Cover).
		SetReleaseDate(detail.ReleaseDate).
		SetAutoDownload(autoDownload).
		SetStatus(subscription.StatusWaiting).
		SetNextCheckAt(now)

	if opts.Zone != "" {
		create.SetZone(opts.Zone)
	}
	if opts.OriginID != nil {
		create.SetOriginID(*opts.OriginID)
	}

	record, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return service.AddMovie(ctx, movieID, opts)
		}
		return Item{}, fmt.Errorf("create movie subscription: %w", err)
	}

	service.tasks.NotifyMonitorChanged()
	service.signal()
	return subscriptionItem(record), nil
}

// Add is backward compatible with legacy monitor API.
func (service *Service) Add(ctx context.Context, movieID string) (Item, error) {
	return service.AddMovie(ctx, movieID, AddMovieOptions{})
}

type AddActorOptions struct {
	Title        string
	Cover        string
	AutoDownload *bool
	Zone         string
}

// AddActor starts watching an actor for new releases.
func (service *Service) AddActor(ctx context.Context, actorID string, opts AddActorOptions) (Item, error) {
	existing, err := service.database.Subscription.Query().
		Where(subscription.KindEQ(subscription.KindActor), subscription.TargetIDEQ(actorID)).Only(ctx)
	if err == nil {
		if existing.Status == subscription.StatusPaused {
			updated, err := existing.Update().SetStatus(subscription.StatusActive).Save(ctx)
			if err != nil {
				return Item{}, err
			}
			service.tasks.NotifyMonitorChanged()
			return subscriptionItem(updated), nil
		}
		return subscriptionItem(existing), nil
	}
	if !ent.IsNotFound(err) {
		return Item{}, fmt.Errorf("find actor subscription: %w", err)
	}

	// Fetch actor works to discover name/avatar and initialize cursor
	movies, err := service.discover.BrowseMovies(ctx, domain.BrowseOptions{
		EntityType: domain.EntityActor,
		EntityID:   actorID,
		Sort:       "release",
		Order:      "desc",
		Page:       1,
		Limit:      40,
	})
	if err != nil && len(opts.Title) == 0 {
		return Item{}, fmt.Errorf("browse actor %s: %w", actorID, err)
	}

	title := opts.Title
	cover := opts.Cover
	var seenIDs []string
	latestRelease := ""

	if len(movies) > 0 {
		latestRelease = movies[0].ReleaseDate
		for _, m := range movies {
			seenIDs = append(seenIDs, m.ID)
			if title == "" || cover == "" {
				for _, a := range m.Actors {
					if a.ID == actorID {
						if title == "" {
							title = a.Name
						}
						if cover == "" {
							cover = a.Avatar
						}
					}
				}
			}
		}
	}
	if title == "" {
		title = actorID
	}

	cfg, _ := service.Config(ctx)
	autoDownload := cfg.ActorAutoDownload
	if opts.AutoDownload != nil {
		autoDownload = *opts.AutoDownload
	}

	cursorData, _ := json.Marshal(ActorCursor{
		LatestReleaseDate: latestRelease,
		SeenMovieIDs:      seenIDs,
	})

	now := time.Now()
	nextCheck := nextActorCheck(now, cfg.ActorCheckTime)

	create := service.database.Subscription.Create().
		SetKind(subscription.KindActor).
		SetTargetID(actorID).
		SetTitle(title).
		SetCover(cover).
		SetAutoDownload(autoDownload).
		SetStatus(subscription.StatusActive).
		SetCursor(string(cursorData)).
		SetNextCheckAt(nextCheck)

	if opts.Zone != "" {
		create.SetZone(opts.Zone)
	}

	record, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return service.AddActor(ctx, actorID, opts)
		}
		return Item{}, fmt.Errorf("create actor subscription: %w", err)
	}

	// Automatically track unreleased / upcoming works of this actor
	today := now.Format("2006-01-02")
	for _, m := range movies {
		if m.ReleaseDate >= today || m.ReleaseDate == "" {
			opts := AddMovieOptions{
				AutoDownload: &autoDownload,
				Zone:         opts.Zone,
				OriginID:     &record.ID,
			}
			_, _ = service.AddMovie(ctx, m.ID, opts)
		}
	}

	service.tasks.NotifyMonitorChanged()
	return subscriptionItem(record), nil
}

type UpdateOptions struct {
	AutoDownload *bool
	Zone         *string
	Status       *string
}

func (service *Service) Update(ctx context.Context, id int, opts UpdateOptions) (Item, error) {
	record, err := service.database.Subscription.Get(ctx, id)
	if err != nil {
		return Item{}, err
	}
	updater := record.Update()
	if opts.AutoDownload != nil {
		updater.SetAutoDownload(*opts.AutoDownload)
	}
	if opts.Zone != nil {
		updater.SetZone(*opts.Zone)
	}
	if opts.Status != nil {
		updater.SetStatus(subscription.Status(*opts.Status))
	}
	updated, err := updater.Save(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("update subscription %d: %w", id, err)
	}
	service.tasks.NotifyMonitorChanged()
	return subscriptionItem(updated), nil
}

func (service *Service) Remove(ctx context.Context, id int) error {
	record, err := service.database.Subscription.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := service.database.Subscription.DeleteOne(record).Exec(ctx); err != nil {
		return fmt.Errorf("remove subscription %d: %w", id, err)
	}
	service.tasks.NotifyMonitorChanged()
	return nil
}

func (service *Service) RemoveByTarget(ctx context.Context, kind string, targetID string) error {
	record, err := service.database.Subscription.Query().
		Where(subscription.KindEQ(subscription.Kind(kind)), subscription.TargetIDEQ(targetID)).Only(ctx)
	if err != nil {
		return err
	}
	if err := service.database.Subscription.DeleteOne(record).Exec(ctx); err != nil {
		return fmt.Errorf("remove subscription %s/%s: %w", kind, targetID, err)
	}
	service.tasks.NotifyMonitorChanged()
	return nil
}

func (service *Service) Retry(ctx context.Context, id int) (Item, error) {
	record, err := service.database.Subscription.Get(ctx, id)
	if err != nil {
		return Item{}, err
	}
	updated, err := record.Update().
		SetStatus(subscription.StatusWaiting).
		SetNextCheckAt(time.Now()).
		SetHash("").
		ClearTaskID().
		ClearError().
		Save(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("retry subscription %d: %w", id, err)
	}
	service.tasks.NotifyMonitorChanged()
	service.signal()
	return subscriptionItem(updated), nil
}

func (service *Service) RetryByTarget(ctx context.Context, movieID string) (Item, error) {
	record, err := service.database.Subscription.Query().
		Where(subscription.KindEQ(subscription.KindMovie), subscription.TargetIDEQ(movieID)).Only(ctx)
	if err != nil {
		return Item{}, err
	}
	return service.Retry(ctx, record.ID)
}

// EnqueueSingle takes a movie subscription, picks the best magnet according to
// preferences, and submits it to 115. If no magnet exists, auto_download is enabled.
func (service *Service) EnqueueSingle(ctx context.Context, id int) (Item, error) {
	record, err := service.database.Subscription.Get(ctx, id)
	if err != nil {
		return Item{}, err
	}
	if record.Kind != subscription.KindMovie {
		return Item{}, errors.New("only movie subscriptions can be enqueued")
	}

	cfg, _ := service.Config(ctx)
	picker := magnet.NewPicker(cfg.Preferences)

	magnets, err := service.discover.CatalogueMagnets(ctx, record.TargetID)
	if err != nil {
		return Item{}, fmt.Errorf("fetch magnets for %s: %w", record.Code, err)
	}

	bestMagnet, found := picker.Pick(magnets)
	now := time.Now()

	if !found {
		// No matching magnet yet; keep waiting and enable auto_download
		updated, err := record.Update().
			SetAutoDownload(true).
			SetStatus(subscription.StatusWaiting).
			SetLastCheckedAt(now).
			ClearError().
			Save(ctx)
		if err != nil {
			return Item{}, err
		}
		service.tasks.NotifyMonitorChanged()
		return subscriptionItem(updated), nil
	}

	// Submit to 115
	submission, err := service.offline.Add(ctx, record.TargetID, bestMagnet.Hash)
	if err != nil {
		return Item{}, fmt.Errorf("submit %s to 115: %w", record.Code, err)
	}

	updated, err := record.Update().
		SetStatus(subscription.StatusAdded).
		SetHash(bestMagnet.Hash).
		SetTaskID(submission.TaskID).
		SetLastCheckedAt(now).
		AddChecks(1).
		ClearNextCheckAt().
		ClearError().
		Save(ctx)
	if err != nil {
		return Item{}, err
	}

	service.tasks.NotifyMonitorChanged()
	return subscriptionItem(updated), nil
}

type BatchEnqueueRequest struct {
	IDs []int `json:"ids"`
	All bool  `json:"all"`
}

// EnqueueBatch creates an asynchronous subscription_batch task.
func (service *Service) EnqueueBatch(ctx context.Context, req BatchEnqueueRequest) (int, error) {
	var ids []int
	if req.All {
		waiting, err := service.database.Subscription.Query().
			Where(subscription.KindEQ(subscription.KindMovie), subscription.StatusEQ(subscription.StatusWaiting)).
			Select(subscription.FieldID).
			Ints(ctx)
		if err != nil {
			return 0, fmt.Errorf("load waiting subscriptions: %w", err)
		}
		ids = waiting
	} else {
		ids = req.IDs
	}

	if len(ids) == 0 {
		return 0, errors.New("no subscriptions selected for batch enqueue")
	}

	payloadBytes, err := json.Marshal(BatchPayload{
		IDs:   ids,
		Total: len(ids),
	})
	if err != nil {
		return 0, err
	}

	taskRow, err := service.database.Task.Create().
		SetType(string(tasks.KindSubscriptionBatch)).
		SetStatus(task.StatusQueued).
		SetProgress(0).
		SetPayload(payloadBytes).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("create subscription batch task: %w", err)
	}

	service.tasks.Notify()
	return taskRow.ID, nil
}

// BatchHandler processes subscription_batch tasks with 1.5s - 3s delay between submissions.
func (service *Service) BatchHandler(ctx context.Context, job tasks.Job) error {
	var payload BatchPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("decode batch payload: %w", err)
	}

	total := len(payload.IDs)
	if total == 0 {
		return nil
	}

	for i, subID := range payload.IDs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if i > 0 {
			// Random delay between 1.5s and 3.0s to respect 115 rate limits
			gap := 1500*time.Millisecond + time.Duration(rand.Intn(1500))*time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(gap):
			}
		}

		_, err := service.EnqueueSingle(ctx, subID)
		if err != nil {
			slog.WarnContext(ctx, "batch subscription enqueue item failed", "id", subID, "error", err)
			payload.FailedCount++
		} else {
			payload.SuccessCount++
		}
		payload.Processed++

		progress := payload.Processed * 100 / total
		updatedPayload, _ := json.Marshal(payload)
		_ = service.database.Task.UpdateOneID(job.ID).
			SetProgress(progress).
			SetPayload(updatedPayload).
			Exec(ctx)

		service.tasks.Notify()
		service.tasks.NotifyMonitorChanged()
	}

	return nil
}

func (service *Service) BatchFinished(ctx context.Context, tx *ent.Tx, job tasks.Job, result error) (tasks.Change, error) {
	return tasks.ChangeOffline | tasks.ChangeMonitor, nil
}

// ActorFeed returns all movie subscriptions spawned by the specified actor.
func (service *Service) ActorFeed(ctx context.Context, actorSubID int, page int, limit int) ([]Item, error) {
	query := service.database.Subscription.Query().
		Where(subscription.KindEQ(subscription.KindMovie), subscription.OriginIDEQ(actorSubID)).
		Order(ent.Desc(subscription.FieldReleaseDate), ent.Desc(subscription.FieldID))

	if limit > 0 {
		if page < 1 {
			page = 1
		}
		query = query.Offset((page - 1) * limit).Limit(limit)
	}

	records, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list actor feed: %w", err)
	}
	result := make([]Item, len(records))
	for index, record := range records {
		result[index] = subscriptionItem(record)
	}
	return result, nil
}

// Check polls due movie and actor subscriptions.
func (service *Service) Check(ctx context.Context) error {
	if err := service.checking.Lock(ctx); err != nil {
		return err
	}
	defer service.checking.Unlock()

	var checkErrors []error

	// 1. Check due movies
	movieRecords, err := service.database.Subscription.Query().
		Where(
			subscription.KindEQ(subscription.KindMovie),
			subscription.StatusEQ(subscription.StatusWaiting),
			subscription.NextCheckAtLTE(time.Now()),
		).
		Order(ent.Asc(subscription.FieldNextCheckAt)).
		Limit(monitorBatchSize).
		All(ctx)
	if err != nil {
		return fmt.Errorf("load due movie subscriptions: %w", err)
	}

	cfg, _ := service.Config(ctx)
	picker := magnet.NewPicker(cfg.Preferences)

	for index, record := range movieRecords {
		if index > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(monitorRequestGap):
			}
		}
		if err := service.checkMovie(ctx, record, picker); err != nil {
			if ctx.Err() != nil {
				return err
			}
			checkErrors = append(checkErrors, err)
		}
	}

	// 2. Check due actors
	actorRecords, err := service.database.Subscription.Query().
		Where(
			subscription.KindEQ(subscription.KindActor),
			subscription.StatusEQ(subscription.StatusActive),
			subscription.NextCheckAtLTE(time.Now()),
		).
		Order(ent.Asc(subscription.FieldNextCheckAt)).
		Limit(5).
		All(ctx)
	if err == nil {
		for index, record := range actorRecords {
			if len(movieRecords) > 0 || index > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(monitorRequestGap):
				}
			}
			if err := service.checkActor(ctx, record, cfg); err != nil {
				if ctx.Err() != nil {
					return err
				}
				checkErrors = append(checkErrors, err)
			}
		}
	}

	return errors.Join(checkErrors...)
}

func (service *Service) checkMovie(ctx context.Context, record *ent.Subscription, picker *magnet.Picker) error {
	now := time.Now()
	magnets, err := service.discover.CatalogueMagnets(ctx, record.TargetID)
	if err != nil {
		return service.deferCheck(ctx, record, now, domain.E(domain.KindUpstream, "查询磁力失败："+domain.PublicMessage(err), err))
	}

	bestMagnet, found := picker.Pick(magnets)
	if !found {
		next, stale := nextMonitorCheck(now, record.ReleaseDate, record.CreatedAt)
		update := record.Update().SetLastCheckedAt(now).AddChecks(1).ClearError()
		if stale {
			update.SetStatus(subscription.StatusStale).ClearNextCheckAt()
		} else {
			update.SetNextCheckAt(next)
		}
		if err := update.Exec(ctx); err != nil && !ent.IsNotFound(err) {
			return fmt.Errorf("schedule subscription %d: %w", record.ID, err)
		}
		if stale {
			service.tasks.NotifyMonitorChanged()
		}
		return nil
	}

	if !record.AutoDownload {
		// Magnet found, but auto download is disabled; keep waiting for user enqueue
		if err := record.Update().
			SetLastCheckedAt(now).
			SetHash(bestMagnet.Hash).
			AddChecks(1).
			ClearError().
			Exec(ctx); err != nil && !ent.IsNotFound(err) {
			return fmt.Errorf("update subscription %d: %w", record.ID, err)
		}
		service.tasks.NotifyMonitorChanged()
		return nil
	}

	submission, err := service.offline.Add(ctx, record.TargetID, bestMagnet.Hash)
	if err != nil {
		return service.deferCheck(ctx, record, now, domain.E(domain.KindUpstream, "加入 115 失败："+domain.PublicMessage(err), err))
	}
	if err := record.Update().
		SetStatus(subscription.StatusAdded).
		SetHash(bestMagnet.Hash).
		SetTaskID(submission.TaskID).
		SetLastCheckedAt(now).
		AddChecks(1).
		ClearNextCheckAt().
		ClearError().
		Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("complete subscription %d: %w", record.ID, err)
	}
	slog.InfoContext(ctx, "subscribed movie submitted to 115", "code", record.Code, "hash", bestMagnet.Hash, "task_id", submission.TaskID)
	service.tasks.NotifyMonitorChanged()
	return nil
}

func (service *Service) checkActor(ctx context.Context, record *ent.Subscription, cfg Config) error {
	now := time.Now()
	movies, err := service.discover.BrowseMovies(ctx, domain.BrowseOptions{
		EntityType: domain.EntityActor,
		EntityID:   record.TargetID,
		Sort:       "release",
		Order:      "desc",
		Page:       1,
		Limit:      40,
	})
	if err != nil {
		return service.deferCheck(ctx, record, now, domain.E(domain.KindUpstream, "查询演员新作失败："+domain.PublicMessage(err), err))
	}

	var cursor ActorCursor
	if record.Cursor != "" {
		_ = json.Unmarshal([]byte(record.Cursor), &cursor)
	}

	seenMap := make(map[string]bool)
	for _, id := range cursor.SeenMovieIDs {
		seenMap[id] = true
	}

	newSeen := append([]string{}, cursor.SeenMovieIDs...)
	latestRelease := cursor.LatestReleaseDate

	for _, m := range movies {
		if !seenMap[m.ID] {
			seenMap[m.ID] = true
			newSeen = append(newSeen, m.ID)

			// Spawn movie subscription
			opts := AddMovieOptions{
				AutoDownload: &record.AutoDownload,
				Zone:         record.Zone,
				OriginID:     &record.ID,
			}
			_, _ = service.AddMovie(ctx, m.ID, opts)

			if m.ReleaseDate > latestRelease {
				latestRelease = m.ReleaseDate
			}
		}
	}

	// Keep seen list bounded to recent 300 IDs
	if len(newSeen) > 300 {
		newSeen = newSeen[len(newSeen)-300:]
	}

	cursor.LatestReleaseDate = latestRelease
	cursor.SeenMovieIDs = newSeen
	cursorBytes, _ := json.Marshal(cursor)

	nextCheck := nextActorCheck(now, cfg.ActorCheckTime)
	if err := record.Update().
		SetCursor(string(cursorBytes)).
		SetLastCheckedAt(now).
		SetNextCheckAt(nextCheck).
		AddChecks(1).
		ClearError().
		Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("update actor subscription %d: %w", record.ID, err)
	}

	service.tasks.NotifyMonitorChanged()
	return nil
}

func (service *Service) deferCheck(ctx context.Context, record *ent.Subscription, now time.Time, cause error) error {
	if err := record.Update().
		SetLastCheckedAt(now).
		SetNextCheckAt(now.Add(monitorRetryInterval)).
		SetError(domain.PublicMessage(cause)).
		Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("defer subscription %d: %w", record.ID, err)
	}
	service.tasks.NotifyMonitorChanged()
	return fmt.Errorf("subscription %s: %w", record.Code, cause)
}

func nextMonitorCheck(now time.Time, releaseDate string, createdAt time.Time) (time.Time, bool) {
	today := startOfDay(now)
	release, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(releaseDate), time.Local)
	if err != nil {
		release = startOfDay(createdAt)
	}
	release = startOfDay(release)
	if today.Before(release) {
		next := now.Add(monitorPreReleaseInterval)
		releaseAt := time.Date(release.Year(), release.Month(), release.Day(),
			now.Hour(), now.Minute(), now.Second(), 0, time.Local)
		if releaseAt.After(now) && releaseAt.Before(next) {
			next = releaseAt
		}
		return next, false
	}
	if today.After(release.AddDate(0, 0, monitorStaleAfterDays)) {
		return time.Time{}, true
	}
	return now.Add(monitorReleasedInterval), false
}

func nextActorCheck(now time.Time, checkTimeStr string) time.Time {
	hour, minute := 4, 0
	if parts := strings.Split(checkTimeStr, ":"); len(parts) == 2 {
		if h, err := strconv.Atoi(parts[0]); err == nil {
			hour = h
		}
		if m, err := strconv.Atoi(parts[1]); err == nil {
			minute = m
		}
	}
	todayCheck := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.Local)
	if todayCheck.After(now) {
		return todayCheck
	}
	return todayCheck.Add(24 * time.Hour)
}

func startOfDay(value time.Time) time.Time {
	value = value.In(time.Local)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.Local)
}

func subscriptionItem(record *ent.Subscription) Item {
	return Item{
		ID:            record.ID,
		Kind:          string(record.Kind),
		TargetID:      record.TargetID,
		Code:          record.Code,
		Title:         record.Title,
		Cover:         record.Cover,
		ReleaseDate:   record.ReleaseDate,
		OriginID:      record.OriginID,
		AutoDownload:  record.AutoDownload,
		Zone:          record.Zone,
		Status:        Status(record.Status),
		Cursor:        record.Cursor,
		Hash:          record.Hash,
		TaskID:        record.TaskID,
		NextCheckAt:   record.NextCheckAt,
		LastCheckedAt: record.LastCheckedAt,
		Checks:        record.Checks,
		Error:         record.Error,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}
}

func loadSetting[T any](ctx context.Context, database *ent.Client, key string) (T, bool, error) {
	var value T
	record, err := database.Setting.Query().Where(setting.Key(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return value, false, nil
	}
	if err != nil {
		return value, false, fmt.Errorf("load setting %s: %w", key, err)
	}
	if err := json.Unmarshal(record.Value, &value); err != nil {
		return value, false, fmt.Errorf("decode setting %s: %w", key, err)
	}
	return value, true, nil
}

func saveSetting(ctx context.Context, database *ent.Client, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode setting %s: %w", key, err)
	}
	if err := database.Setting.Create().SetKey(key).SetValue(jsontext.Value(encoded)).
		OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}
	return nil
}
