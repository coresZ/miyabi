package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/ppxb/miyabi/internal/tasks"
	"log/slog"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/monitor"
	"github.com/ppxb/miyabi/internal/syncx"
)

const (
	// Movies are polled every three days before release, daily from the
	// release date, and abandoned once 30 days pass without a magnet.
	monitorPreReleaseInterval = 72 * time.Hour
	monitorReleasedInterval   = 24 * time.Hour
	monitorRetryInterval      = time.Hour
	monitorStaleAfterDays     = 30
	monitorBatchSize          = 10
	monitorRequestGap         = 2 * time.Second
)

type MonitorItem struct {
	ID            int            `json:"id"`
	MovieID       string         `json:"movie_id"`
	Code          string         `json:"code"`
	Title         string         `json:"title"`
	Cover         string         `json:"cover"`
	ReleaseDate   string         `json:"release_date"`
	Status        monitor.Status `json:"status"`
	Hash          string         `json:"hash,omitempty"`
	TaskID        *int           `json:"task_id,omitempty"`
	NextCheckAt   *time.Time     `json:"next_check_at,omitempty"`
	LastCheckedAt *time.Time     `json:"last_checked_at,omitempty"`
	Checks        int            `json:"checks"`
	Error         *string        `json:"error,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// MonitorService keeps a watch list of unreleased movies and submits the
// first magnet to 115 once JavDB indexes one.
type MonitorService struct {
	database *ent.Client
	discover *DiscoverService
	offline  *OfflineService
	tasks    *tasks.Service
	checking syncx.ContextLock
	wake     chan struct{}
}

func NewMonitorService(database *ent.Client, discover *DiscoverService, offline *OfflineService, tasks *tasks.Service) *MonitorService {
	return &MonitorService{
		database: database, discover: discover, offline: offline, tasks: tasks,
		wake: make(chan struct{}, 1),
	}
}

// Pending signals when a monitor wants an immediate check.
func (service *MonitorService) Pending() <-chan struct{} {
	return service.wake
}

func (service *MonitorService) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

func (service *MonitorService) List(ctx context.Context) ([]MonitorItem, error) {
	records, err := service.database.Monitor.Query().Order(ent.Desc(monitor.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	result := make([]MonitorItem, len(records))
	for index, record := range records {
		result[index] = monitorItem(record)
	}
	return result, nil
}

// Add starts watching a movie. Re-adding a stale or added monitor resets it
// so the next poll checks again.
func (service *MonitorService) Add(ctx context.Context, movieID string) (MonitorItem, error) {
	existing, err := service.database.Monitor.Query().Where(monitor.MovieIDEQ(movieID)).Only(ctx)
	if err == nil {
		if existing.Status == monitor.StatusWaiting {
			return monitorItem(existing), nil
		}
		return service.Retry(ctx, movieID)
	}
	if !ent.IsNotFound(err) {
		return MonitorItem{}, fmt.Errorf("find monitor: %w", err)
	}
	detail, err := service.discover.MovieDetail(ctx, movieID)
	if err != nil {
		return MonitorItem{}, err
	}
	now := time.Now()
	record, err := service.database.Monitor.Create().
		SetMovieID(detail.ID).SetCode(detail.Code).SetTitle(detail.Title).SetCover(detail.Cover).
		SetReleaseDate(detail.ReleaseDate).SetNextCheckAt(now).Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return service.Add(ctx, movieID)
		}
		return MonitorItem{}, fmt.Errorf("create monitor: %w", err)
	}
	service.tasks.NotifyMonitorChanged()
	service.signal()
	return monitorItem(record), nil
}

func (service *MonitorService) Remove(ctx context.Context, movieID string) error {
	record, err := service.database.Monitor.Query().Where(monitor.MovieIDEQ(movieID)).Only(ctx)
	if err != nil {
		return err
	}
	if err := service.database.Monitor.DeleteOne(record).Exec(ctx); err != nil {
		return fmt.Errorf("remove monitor: %w", err)
	}
	service.tasks.NotifyMonitorChanged()
	return nil
}

// Retry re-arms a stale or added monitor for an immediate check.
func (service *MonitorService) Retry(ctx context.Context, movieID string) (MonitorItem, error) {
	record, err := service.database.Monitor.Query().Where(monitor.MovieIDEQ(movieID)).Only(ctx)
	if err != nil {
		return MonitorItem{}, err
	}
	record, err = record.Update().SetStatus(monitor.StatusWaiting).SetNextCheckAt(time.Now()).
		SetHash("").ClearTaskID().ClearError().Save(ctx)
	if err != nil {
		return MonitorItem{}, fmt.Errorf("retry monitor: %w", err)
	}
	service.tasks.NotifyMonitorChanged()
	service.signal()
	return monitorItem(record), nil
}

// Check polls due monitors in small batches with a pause between JavDB
// requests. Overlapping runs are serialized.
func (service *MonitorService) Check(ctx context.Context) error {
	if err := service.checking.Lock(ctx); err != nil {
		return err
	}
	defer service.checking.Unlock()
	records, err := service.database.Monitor.Query().
		Where(monitor.StatusEQ(monitor.StatusWaiting), monitor.NextCheckAtLTE(time.Now())).
		Order(ent.Asc(monitor.FieldNextCheckAt)).Limit(monitorBatchSize).All(ctx)
	if err != nil {
		return fmt.Errorf("load due monitors: %w", err)
	}
	var checkErrors []error
	for index, record := range records {
		if index > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(monitorRequestGap):
			}
		}
		if err := service.checkOne(ctx, record); err != nil {
			if ctx.Err() != nil {
				return err
			}
			checkErrors = append(checkErrors, err)
		}
	}
	return errors.Join(checkErrors...)
}

func (service *MonitorService) checkOne(ctx context.Context, record *ent.Monitor) error {
	now := time.Now()
	magnets, err := service.discover.Magnets(ctx, record.MovieID)
	if err != nil {
		return service.deferCheck(ctx, record, now, domain.E(domain.KindUpstream, "查询磁力失败："+domain.PublicMessage(err), err))
	}
	if len(magnets) == 0 {
		next, stale := nextMonitorCheck(now, record.ReleaseDate, record.CreatedAt)
		update := record.Update().SetLastCheckedAt(now).AddChecks(1).ClearError()
		if stale {
			update.SetStatus(monitor.StatusStale).ClearNextCheckAt()
		} else {
			update.SetNextCheckAt(next)
		}
		if err := update.Exec(ctx); err != nil && !ent.IsNotFound(err) {
			return fmt.Errorf("schedule monitor %d: %w", record.ID, err)
		}
		if stale {
			service.tasks.NotifyMonitorChanged()
		}
		return nil
	}
	hash := strings.ToLower(magnets[0].Hash)
	submission, err := service.offline.Add(ctx, record.MovieID, hash)
	if err != nil {
		return service.deferCheck(ctx, record, now, domain.E(domain.KindUpstream, "加入 115 失败："+domain.PublicMessage(err), err))
	}
	if err := record.Update().SetStatus(monitor.StatusAdded).SetHash(hash).SetTaskID(submission.TaskID).
		SetLastCheckedAt(now).AddChecks(1).ClearNextCheckAt().ClearError().Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("complete monitor %d: %w", record.ID, err)
	}
	slog.InfoContext(ctx, "monitored movie submitted to 115", "code", record.Code, "hash", hash, "task_id", submission.TaskID)
	service.tasks.NotifyMonitorChanged()
	return nil
}

// deferCheck records a transient failure and retries within the hour rather
// than consuming the daily slot. The record stores the public message only;
// the returned error keeps the full cause chain for logs.
func (service *MonitorService) deferCheck(ctx context.Context, record *ent.Monitor, now time.Time, cause error) error {
	if err := record.Update().SetLastCheckedAt(now).SetNextCheckAt(now.Add(monitorRetryInterval)).
		SetError(domain.PublicMessage(cause)).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("defer monitor %d: %w", record.ID, err)
	}
	service.tasks.NotifyMonitorChanged()
	return fmt.Errorf("monitor %s: %w", record.Code, cause)
}

// nextMonitorCheck applies the polling policy: every three days before the
// release date (but never past the release day itself), daily for 30 days
// from release, then stale. An unparsable release date counts from creation.
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

func startOfDay(value time.Time) time.Time {
	value = value.In(time.Local)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.Local)
}

func monitorItem(record *ent.Monitor) MonitorItem {
	item := MonitorItem{
		ID: record.ID, MovieID: record.MovieID, Code: record.Code, Title: record.Title, Cover: record.Cover,
		ReleaseDate: record.ReleaseDate, Status: record.Status, Hash: record.Hash, TaskID: record.TaskID,
		NextCheckAt: record.NextCheckAt, LastCheckedAt: record.LastCheckedAt, Checks: record.Checks,
		Error: record.Error, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
	return item
}
