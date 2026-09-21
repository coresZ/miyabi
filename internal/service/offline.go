package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/syncx"
	"github.com/ppxb/miyabi/internal/tasks"
)

var ErrMagnetNotFound = domain.E(domain.KindInvalid, "磁力链不属于当前影片，请刷新后重试", nil)

type OfflineSubmission struct {
	TaskID      int         `json:"task_id"`
	Code        string      `json:"code"`
	JavDBID     string      `json:"javdb_id"`
	LibraryID   int         `json:"library_id,omitempty"`
	AccountID   string      `json:"account_id"`
	DirectoryID string      `json:"directory_id"`
	ScanTaskID  int         `json:"scan_task_id,omitempty"`
	Hash        string      `json:"hash"`
	Status      task.Status `json:"status"`
	Phase       string      `json:"phase"`
	Processing  bool        `json:"processing"`
	Progress    int         `json:"progress"`
	Error       *string     `json:"error,omitempty"`
}

type OfflineActivity struct {
	Source *domain.LibrarySource `json:"source,omitempty"`
	Tasks  []OfflineSubmission   `json:"tasks"`
}

type offlinePayload struct {
	Code             string   `json:"code"`
	JavDBID          string   `json:"javdb_id"`
	Hash             string   `json:"hash"`
	InfoHash         string   `json:"info_hash"`
	AccountID        string   `json:"account_id"`
	DirectoryID      string   `json:"directory_id"`
	FileID           string   `json:"file_id,omitempty"`
	FileIDs          []string `json:"file_ids,omitempty"`
	ScanTaskID       int      `json:"scan_task_id,omitempty"`
	AwaitingLocation bool     `json:"awaiting_location,omitempty"`
}

type OfflineService struct {
	database   *ent.Client
	discover   *DiscoverService
	drive      *drive.Drive
	tasks      *tasks.Service
	operations offlineOperations
	syncing    syncx.ContextLock
}

func NewOfflineService(database *ent.Client, discover *DiscoverService, drive *drive.Drive, tasks *tasks.Service) *OfflineService {
	return &OfflineService{database: database, discover: discover, drive: drive, tasks: tasks}
}

func (service *OfflineService) Add(ctx context.Context, movieID, hash string) (OfflineSubmission, error) {
	hash = strings.ToLower(hash)
	magnets, err := service.discover.Magnets(ctx, movieID)
	if err != nil {
		return OfflineSubmission{}, err
	}
	if !slices.ContainsFunc(magnets, func(magnet DiscoverMagnet) bool { return magnet.Hash == hash }) {
		return OfflineSubmission{}, ErrMagnetNotFound
	}
	movie, err := service.discover.MovieDetail(ctx, movieID)
	if err != nil {
		return OfflineSubmission{}, err
	}
	code := codeid.Normalize(movie.Code)
	sess, err := service.drive.Open(ctx)
	if err != nil {
		return OfflineSubmission{}, fmt.Errorf("get 115 account for offline download: %w", err)
	}
	source := sess.Source()
	directory := source.Directory
	unlock, err := service.operations.Lock(ctx, source.AccountID, hash)
	if err != nil {
		return OfflineSubmission{}, err
	}
	defer unlock()
	existing, err := service.database.Task.Query().Where(task.TypeEQ(tasks.KindOffline.String()),
		task.StatusIn(task.StatusQueued, task.StatusRunning), func(s *sql.Selector) {
			s.Where(sql.And(
				sqljson.ValueEQ(task.FieldPayload, source.AccountID, sqljson.Path(tasks.PathAccountID)),
				sqljson.ValueEQ(task.FieldPayload, hash, sqljson.Path(tasks.PathHash)),
			))
		}).First(ctx)
	if err == nil {
		input, err := tasks.DecodePayload[offlinePayload](existing.Payload)
		if err != nil {
			return OfflineSubmission{}, err
		}
		if input.DirectoryID != directory.ID {
			return OfflineSubmission{}, domain.E(domain.KindConflict, "该磁力正在下载到另一个目录，请先在 115 中处理该任务", nil)
		}
		return service.submission(ctx, existing, &source)
	}
	if !ent.IsNotFound(err) {
		return OfflineSubmission{}, fmt.Errorf("find active offline task: %w", err)
	}
	previous, err := service.database.Task.Query().Where(task.TypeEQ(tasks.KindOffline.String()), task.StatusEQ(task.StatusDone), func(s *sql.Selector) {
		s.Where(sql.And(
			sqljson.ValueEQ(task.FieldPayload, source.AccountID, sqljson.Path(tasks.PathAccountID)),
			sqljson.ValueEQ(task.FieldPayload, directory.ID, sqljson.Path(tasks.PathDirectoryID)),
			sqljson.ValueEQ(task.FieldPayload, hash, sqljson.Path(tasks.PathHash)),
		))
	}).Order(ent.Desc(task.FieldID)).First(ctx)
	if err == nil {
		state, err := service.submission(ctx, previous, &source)
		if err != nil {
			return OfflineSubmission{}, err
		}
		if state.Processing {
			return state, nil
		}
	} else if !ent.IsNotFound(err) {
		return OfflineSubmission{}, fmt.Errorf("find download workflow: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return OfflineSubmission{}, err
	}
	done, ok := service.drive.StartWork()
	if !ok {
		return OfflineSubmission{}, context.Canceled
	}
	defer done()
	// Once a remote mutation starts, finish recording it even if the tab closes.
	submitContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	remote, err := service.submit(submitContext, sess, hash)
	if err != nil {
		return OfflineSubmission{}, fmt.Errorf("submit 115 offline download: %w", err)
	}
	input := offlinePayload{Code: code, JavDBID: movie.ID, Hash: hash, InfoHash: remote.Hash,
		AccountID: source.AccountID, DirectoryID: directory.ID}
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		return OfflineSubmission{}, err
	}
	var created *ent.Task
	if err := service.drive.Commit(submitContext, func(tx *ent.Tx) error {
		var err error
		created, err = tx.Task.Create().SetType(tasks.KindOffline.String()).SetStatus(task.StatusRunning).SetPayload(encoded).Save(submitContext)
		if err != nil {
			return err
		}
		if remote.Status == 2 {
			return service.completeTask(submitContext, tx, created, input, remote.FileID, sess)
		}
		return nil
	}); err != nil {
		return OfflineSubmission{}, fmt.Errorf("record 115 offline download: %w", err)
	}
	service.tasks.NotifyOfflineChanged()
	created, err = service.database.Task.Get(submitContext, created.ID)
	if err != nil {
		return OfflineSubmission{}, err
	}
	return service.submission(submitContext, created, &source)
}

// submit handles duplicate history by inspecting its real output. Only a
// terminal task with confirmed absent video content is removed, never files.
// The caller holds the account/hash lock, never the shared Pan state lock.
func (service *OfflineService) submit(ctx context.Context, sess drive.Session, hash string) (pan.OfflineTask, error) {
	infoHash, err := sess.AddOffline(ctx, "magnet:?xt=urn:btih:"+hash)
	if err == nil {
		return pan.OfflineTask{Hash: infoHash}, nil
	}
	if !errors.Is(err, pan.ErrOfflineExists) {
		return pan.OfflineTask{}, err
	}
	remote, err := service.findRemoteTask(ctx, sess, hash)
	if err != nil {
		return pan.OfflineTask{}, err
	}
	source := sess.Source()
	if remote.Status == 0 || remote.Status == 1 {
		if remote.DirectoryID != source.Directory.ID {
			return pan.OfflineTask{}, domain.E(domain.KindConflict, "115 已有该磁力的下载任务，目标目录与当前媒体目录不一致", nil)
		}
		return remote, nil
	}
	if remote.Status != 2 && remote.Status != -1 {
		return pan.OfflineTask{}, fmt.Errorf("115 returned unknown offline status %d", remote.Status)
	}
	if remote.FileID == "" {
		return pan.OfflineTask{}, domain.E(domain.KindConflict, "115 的历史任务未提供资源位置，请先在 115 客户端清理该任务记录", nil)
	}
	present, err := service.remoteHasVideo(ctx, sess, remote.FileID)
	if err != nil {
		return pan.OfflineTask{}, err
	}
	if present {
		if remote.Status == -1 {
			return pan.OfflineTask{}, domain.E(domain.KindConflict, "115 任务失败但目录内仍有视频，请先在 115 客户端确认完整性", nil)
		}
		return remote, nil
	}
	if err := sess.RemoveOffline(ctx, remote.Hash); err != nil {
		return pan.OfflineTask{}, fmt.Errorf("remove stale 115 task history: %w", err)
	}
	infoHash, err = sess.AddOffline(ctx, "magnet:?xt=urn:btih:"+hash)
	return pan.OfflineTask{Hash: infoHash}, err
}

func (service *OfflineService) findRemoteTask(ctx context.Context, sess drive.Session, hash string) (pan.OfflineTask, error) {
	var found *pan.OfflineTask
	err := drive.WalkOfflinePages(ctx, func(page int) (pan.OfflinePage, error) {
		return sess.OfflineTasks(ctx, page)
	}, func(remote pan.OfflinePage) (bool, error) {
		for _, download := range remote.Tasks {
			if strings.EqualFold(download.Hash, hash) {
				found = &download
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return pan.OfflineTask{}, fmt.Errorf("find duplicate 115 task: %w", err)
	}
	if found != nil {
		return *found, nil
	}
	return pan.OfflineTask{}, domain.E(domain.KindBusy, "115 提示任务已存在，但任务列表中未找到它，请稍后重试", nil)
}

func (service *OfflineService) remoteHasVideo(ctx context.Context, sess drive.Session, id string) (bool, error) {
	info, err := sess.Info(ctx, id)
	if errors.Is(err, pan.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check existing 115 resource: %w", err)
	}
	source := sess.Source()
	if !drive.WithinSource(info, source) {
		return false, domain.E(domain.KindConflict, "该磁力的资源已在媒体目录之外，请先在 115 中移动资源", nil)
	}
	if !info.IsDirectory {
		return isVideo(info.Name), nil
	}
	directories := []string{info.ID}
	seen := map[string]bool{info.ID: true}
	found := false
	for next := 0; next < len(directories) && !found; next++ {
		dirID := directories[next]
		err := drive.WalkFilePages(ctx, func(offset int) (pan.FilePage, error) {
			return sess.List(ctx, dirID, offset)
		}, func(page pan.FilePage) (bool, error) {
			if !slices.ContainsFunc(page.Path, func(dir pan.Directory) bool { return dir.ID == source.Directory.ID }) {
				return false, domain.E(domain.KindNotFound, "下载目录已移出媒体目录", nil)
			}
			for _, entry := range page.Files {
				if entry.IsDirectory {
					if !seen[entry.ID] {
						seen[entry.ID] = true
						directories = append(directories, entry.ID)
					}
				} else if isVideo(entry.Name) {
					found = true
					return false, nil
				}
			}
			return true, nil
		})
		if err != nil {
			return false, fmt.Errorf("check downloaded video files: %w", err)
		}
	}
	return found, nil
}

func (service *OfflineService) submission(ctx context.Context, record *ent.Task, source *domain.LibrarySource) (OfflineSubmission, error) {
	items, err := service.submissions(ctx, []*ent.Task{record}, source)
	if err != nil {
		return OfflineSubmission{}, err
	}
	return items[0], nil
}

// Project task workflows and file presence in batches. The global observer and
// movie buttons share this view without a database query for every download.
func (service *OfflineService) submissions(ctx context.Context, records []*ent.Task, source *domain.LibrarySource) ([]OfflineSubmission, error) {
	inputs := make([]offlinePayload, len(records))
	var scanIDs []int
	var fileIDs []string
	for index, record := range records {
		input, err := tasks.DecodePayload[offlinePayload](record.Payload)
		if err != nil {
			return nil, err
		}
		if input.Hash == "" {
			return nil, fmt.Errorf("offline task %d is missing magnet hash", record.ID)
		}
		inputs[index] = input
		if source == nil || source.AccountID != input.AccountID || source.Directory.ID != input.DirectoryID ||
			record.Status == task.StatusQueued || record.Status == task.StatusRunning {
			continue
		}
		if input.ScanTaskID != 0 {
			scanIDs = append(scanIDs, input.ScanTaskID)
		}
		fileIDs = append(fileIDs, input.FileIDs...)
	}
	scans := make(map[int]tasks.TaskInfo)
	for start := 0; start < len(scanIDs); start += 500 {
		parents, err := service.database.Task.Query().Where(task.IDIn(scanIDs[start:min(start+500, len(scanIDs))]...)).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("read download scan tasks: %w", err)
		}
		infos, err := service.tasks.Workflows(ctx, parents)
		if err != nil {
			return nil, err
		}
		for _, info := range infos {
			scans[info.ID] = info
		}
	}
	indexed := make(map[string]*ent.Movie)
	for start := 0; start < len(fileIDs); start += 500 {
		files, err := service.database.File.Query().Where(libraryFiles(*source),
			file.FileIDIn(fileIDs[start:min(start+500, len(fileIDs))]...)).
			Select(file.FieldFileID, file.FieldMovieID).
			WithMovie(func(query *ent.MovieQuery) {
				query.Select(movie.FieldID, movie.FieldCode, movie.FieldJavdbID, movie.FieldScrapeStatus)
			}).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("read downloaded file index: %w", err)
		}
		for _, entry := range files {
			indexed[entry.FileID] = entry.Edges.Movie
		}
	}
	result := make([]OfflineSubmission, len(records))
	for index, record := range records {
		input := inputs[index]
		item := &result[index]
		*item = OfflineSubmission{TaskID: record.ID, Code: input.Code, JavDBID: input.JavDBID,
			AccountID: input.AccountID, DirectoryID: input.DirectoryID, ScanTaskID: input.ScanTaskID,
			Hash: input.Hash, Status: record.Status, Progress: record.Progress, Error: record.Error, Phase: "available"}
		if source == nil || source.AccountID != input.AccountID || source.Directory.ID != input.DirectoryID {
			continue
		}
		if record.Status == task.StatusQueued || record.Status == task.StatusRunning {
			item.Phase = "downloading"
			continue
		}
		if input.ScanTaskID != 0 {
			scan, found := scans[input.ScanTaskID]
			if !found {
				return nil, fmt.Errorf("scan task %d for download %d was not found", input.ScanTaskID, record.ID)
			}
			if scan.Status == task.StatusQueued || scan.Status == task.StatusRunning {
				item.Processing = true
			}
			if scan.Error != nil {
				item.Error = scan.Error
			}
		} else if record.Status == task.StatusDone && (input.FileID != "" || input.AwaitingLocation) {
			// Remote completion may arrive before its file location. Keep the
			// indexing workflow active without presenting it as a download.
			item.Processing = true
		}
		if item.Processing {
			item.Phase = "processing"
		}
		for _, id := range input.FileIDs {
			matched, present := indexed[id]
			if !present {
				continue
			}
			if matched != nil && (matched.JavdbID != nil && *matched.JavdbID == input.JavDBID ||
				matched.JavdbID == nil && matched.Code == codeid.Normalize(input.Code)) {
				item.Phase = "in_library"
				item.LibraryID = matched.ID
				if matched.ScrapeStatus != movie.ScrapeStatusFailed {
					item.Error = nil
				}
				break
			}
			if !item.Processing {
				item.Phase = "downloaded"
			}
		}
	}
	return result, nil
}

// Activity only reads local tasks and the mounted file index. Keeping each
// Activity only reads local tasks and the mounted file index. Keeping each
// magnet's latest workflow also retains long downloads until their final state.
func (service *OfflineService) Activity(ctx context.Context) (OfflineActivity, error) {
	result := OfflineActivity{Tasks: []OfflineSubmission{}}
	source := service.drive.Source()
	result.Source = source
	if source == nil {
		return result, nil
	}
	records, err := latestOfflineTasks(ctx, service.database.Task.Query().Where(task.TypeEQ(tasks.KindOffline.String()), func(s *sql.Selector) {
		s.Where(sql.And(
			sqljson.ValueEQ(task.FieldPayload, source.AccountID, sqljson.Path(tasks.PathAccountID)),
			sqljson.ValueEQ(task.FieldPayload, source.Directory.ID, sqljson.Path(tasks.PathDirectoryID)),
		))
	}))
	if err != nil {
		return result, fmt.Errorf("load offline activity: %w", err)
	}
	result.Tasks, err = service.submissions(ctx, records, source)
	return result, err
}

// Tasks projects history through the current file index and workflow. A
// finished remote task alone never means the resource still exists.
func (service *OfflineService) Tasks(ctx context.Context, movieID, accountID string) ([]OfflineSubmission, error) {
	records, err := latestOfflineTasks(ctx, service.database.Task.Query().Where(task.TypeEQ(tasks.KindOffline.String()), func(s *sql.Selector) {
		s.Where(sql.And(
			sqljson.ValueEQ(task.FieldPayload, movieID, sqljson.Path(tasks.PathJavDBID)),
			sqljson.ValueEQ(task.FieldPayload, accountID, sqljson.Path(tasks.PathAccountID)),
		))
	}))
	if err != nil {
		return nil, fmt.Errorf("load movie offline tasks: %w", err)
	}
	source := service.drive.Source()
	return service.submissions(ctx, records, source)
}

func latestOfflineTasks(ctx context.Context, query *ent.TaskQuery) ([]*ent.Task, error) {
	// Apply the same account/movie/directory scope before grouping. No history
	// limit: a long-running download may be older than every completed task.
	return query.Where(func(s *sql.Selector) {
		payload := s.C(task.FieldPayload)
		// A malformed hash must remain visible to validation even if its JSON
		// representation matches a newer string hash.
		latest := s.Clone().Select(sql.Max(s.C(task.FieldID))).
			GroupBy("json_type("+payload+", '$."+tasks.PathHash+"')", tasks.JSONExtract(payload, tasks.PathHash))
		s.Where(sql.In(s.C(task.FieldID), latest))
	}).Order(ent.Desc(task.FieldID)).All(ctx)
}

func (service *OfflineService) Sync(ctx context.Context) error {
	if err := service.syncing.Lock(ctx); err != nil {
		return err
	}
	defer service.syncing.Unlock()
	source := service.drive.Source()
	if source == nil {
		return nil
	}
	sess, err := service.drive.OpenSource(ctx, *source)
	if err != nil {
		if errors.Is(err, drive.ErrMediaDirectoryRequired) {
			return nil
		}
		return err
	}
	records, err := service.database.Task.Query().Where(task.TypeEQ(tasks.KindOffline.String()), task.Or(
		task.StatusIn(task.StatusQueued, task.StatusRunning),
		task.And(task.StatusEQ(task.StatusDone), func(s *sql.Selector) {
			s.Where(sql.And(
				sqljson.ValueEQ(task.FieldPayload, source.AccountID, sqljson.Path(tasks.PathAccountID)),
				sqljson.ValueEQ(task.FieldPayload, source.Directory.ID, sqljson.Path(tasks.PathDirectoryID)),
				sql.Not(sqljson.HasKey(task.FieldPayload, sqljson.Path(tasks.PathScanTaskID))),
				sql.Or(
					sqljson.HasKey(task.FieldPayload, sqljson.Path(tasks.PathFileID)),
					sqljson.ValueEQ(task.FieldPayload, true, sqljson.Path(tasks.PathAwaitingLocation)),
				),
			))
		}),
	)).Order(ent.Desc(task.FieldID)).All(ctx)
	if err != nil {
		return fmt.Errorf("load offline tasks: %w", err)
	}
	if len(records) == 0 {
		return nil
	}
	wanted := make(map[string]*ent.Task)
	seen := make(map[string]bool)
	var syncErrors []error
	for _, record := range records {
		input, err := tasks.DecodePayload[offlinePayload](record.Payload)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Errorf("read offline task %d: %w", record.ID, err))
			continue
		}
		if input.AccountID != source.AccountID {
			continue
		}
		hash := strings.ToLower(input.Hash)
		if seen[hash] {
			continue
		}
		seen[hash] = true
		if record.Status == task.StatusDone {
			if input.DirectoryID != source.Directory.ID {
				continue
			}
			if input.FileID != "" {
				if err := service.updateTask(ctx, sess, record, pan.OfflineTask{Status: 2, FileID: input.FileID, Hash: input.InfoHash}); err != nil {
					syncErrors = append(syncErrors, err)
				}
				continue
			}
		}
		wanted[strings.ToLower(input.InfoHash)] = record
	}
	if len(wanted) > 0 {
		if err := drive.WalkOfflinePages(ctx, func(page int) (pan.OfflinePage, error) {
			return sess.OfflineTasks(ctx, page)
		}, func(remote pan.OfflinePage) (bool, error) {
			for _, download := range remote.Tasks {
				if err := ctx.Err(); err != nil {
					return false, err
				}
				key := strings.ToLower(download.Hash)
				record, ok := wanted[key]
				if !ok {
					continue
				}
				// A task we found is never missing, even if its update fails.
				// Keep syncing other tasks and retry this one on the next poll.
				delete(wanted, key)
				if err := service.updateTask(ctx, sess, record, download); err != nil {
					if errors.Is(err, drive.ErrSourceChanged) || errors.Is(err, pan.ErrUnauthorized) {
						return false, err
					}
					syncErrors = append(syncErrors, err)
				}
			}
			return len(wanted) > 0, nil
		}); err != nil {
			// Do not mark unseen tasks missing after an incomplete listing.
			return errors.Join(append(syncErrors, fmt.Errorf("sync 115 offline tasks: %w", err))...)
		}
	}
	for _, record := range wanted {
		if err := service.markMissing(ctx, sess, record); err != nil {
			syncErrors = append(syncErrors, err)
		}
	}
	return errors.Join(syncErrors...)
}

func (service *OfflineService) updateTask(ctx context.Context, sess drive.Session, record *ent.Task, remote pan.OfflineTask) error {
	input, err := tasks.DecodePayload[offlinePayload](record.Payload)
	if err != nil {
		return err
	}
	unlock, err := service.operations.Lock(ctx, input.AccountID, strings.ToLower(input.Hash))
	if err != nil {
		return err
	}
	defer unlock()

	notify := false
	err = sess.CommitAccount(ctx, func(tx *ent.Tx) error {
		current, err := tx.Task.Get(ctx, record.ID)
		if err != nil {
			return err
		}
		// A remote page may have started loading before another completion
		// committed. Never regress terminal state or overwrite newer payload.
		if current.Status == task.StatusFailed || current.Status == task.StatusDone && remote.Status != 2 {
			return nil
		}
		currentInput, err := tasks.DecodePayload[offlinePayload](current.Payload)
		if err != nil {
			return err
		}
		if currentInput.AccountID != input.AccountID || currentInput.InfoHash != input.InfoHash {
			return fmt.Errorf("offline task identity changed while syncing")
		}
		if remote.Hash != "" && !strings.EqualFold(remote.Hash, currentInput.InfoHash) {
			return fmt.Errorf("115 returned a different offline task than requested")
		}
		status := task.StatusRunning
		switch remote.Status {
		case 0, 1:
		case 2:
			if current.Status == task.StatusDone && (currentInput.ScanTaskID != 0 ||
				currentInput.FileID == "" && remote.FileID == "") {
				return nil
			}
			notify = true
			return service.completeTask(ctx, tx, current, currentInput, remote.FileID, sess)
		case -1:
			status = task.StatusFailed
		default:
			return fmt.Errorf("115 returned unknown offline status %d", remote.Status)
		}
		if current.Status == status && current.Progress == remote.Progress {
			return nil
		}
		update := tx.Task.UpdateOneID(current.ID).SetStatus(status).SetProgress(remote.Progress)
		if status == task.StatusFailed {
			update.SetError("115 离线下载失败，请在 115 客户端查看原因")
		}
		notify = true
		return update.Exec(ctx)
	})
	if err != nil {
		return fmt.Errorf("update offline task %d: %w", record.ID, err)
	}
	if notify {
		service.tasks.NotifyOfflineChanged()
	}
	return nil
}

func (service *OfflineService) markMissing(ctx context.Context, sess drive.Session, record *ent.Task) error {
	input, err := tasks.DecodePayload[offlinePayload](record.Payload)
	if err != nil {
		return err
	}
	unlock, err := service.operations.Lock(ctx, input.AccountID, strings.ToLower(input.Hash))
	if err != nil {
		return err
	}
	defer unlock()

	changed := false
	err = sess.CommitAccount(ctx, func(tx *ent.Tx) error {
		current, err := tx.Task.Get(ctx, record.ID)
		if err != nil {
			return err
		}
		update := tx.Task.UpdateOneID(current.ID)
		switch current.Status {
		case task.StatusQueued, task.StatusRunning:
			update.SetStatus(task.StatusFailed).SetError("115 中未找到该任务，请在 115 客户端确认下载结果")
		case task.StatusDone:
			pending, err := tasks.DecodePayload[offlinePayload](current.Payload)
			if err != nil {
				return err
			}
			if !pending.AwaitingLocation || pending.FileID != "" || pending.ScanTaskID != 0 {
				return nil
			}
			pending.AwaitingLocation = false
			encoded, err := tasks.EncodePayload(pending)
			if err != nil {
				return err
			}
			update.SetPayload(encoded).SetError("115 已完成下载，但任务记录已移除，无法获取文件位置，请扫描媒体目录确认下载结果")
		default:
			return nil
		}
		changed = true
		return update.Exec(ctx)
	})
	if err == nil && changed {
		service.tasks.NotifyOfflineChanged()
	}
	return err
}

// Completion and targeted scan creation are one transaction. Never merge into
// a running scan: it might already have passed the newly downloaded directory.
func (service *OfflineService) completeTask(ctx context.Context, tx *ent.Tx, record *ent.Task, input offlinePayload, fileID string, sess drive.Session) error {
	// A delayed completion must keep the target already recorded by a newer
	// result, including when its scan was deferred until the mount returns.
	if record.Status == task.StatusDone && input.FileID != "" {
		fileID = input.FileID
	}
	input.FileID = fileID
	input.AwaitingLocation = fileID == ""
	currentSource := service.drive.Source()
	// Save remote completion immediately; a later sync will create the scan
	// once 115 exposes the output location.
	if fileID != "" && input.ScanTaskID == 0 && currentSource != nil &&
		currentSource.Directory.ID == input.DirectoryID && currentSource.AccountID == input.AccountID {
		encoded, err := tasks.EncodePayload(scanPayload{
			Source:   *currentSource,
			Scan:     domain.ScanProgress{Stage: "queued", CurrentPath: currentSource.Directory.Path},
			TargetID: fileID, OfflineTaskID: record.ID, Code: input.Code, JavDBID: input.JavDBID,
		})
		if err != nil {
			return err
		}
		scan, err := tx.Task.Create().SetType(tasks.KindScan.String()).SetPayload(encoded).Save(ctx)
		if err != nil {
			return err
		}
		input.ScanTaskID = scan.ID
	}
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		return err
	}
	return tx.Task.UpdateOneID(record.ID).SetStatus(task.StatusDone).SetProgress(100).ClearError().SetPayload(encoded).Exec(ctx)
}
