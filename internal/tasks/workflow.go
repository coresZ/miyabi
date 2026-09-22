package tasks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/task"
)

// TaskInfo is the API presentation of a scan workflow and folded background tasks.
type TaskInfo struct {
	ID            int                  `json:"id"`
	Type          string               `json:"type"`
	Status        task.Status          `json:"status"`
	Progress      int                  `json:"progress"`
	Error         *string              `json:"error,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
	Source        domain.LibrarySource `json:"source"`
	Scan          domain.ScanProgress  `json:"scan"`
	OfflineTaskID int                  `json:"offline_task_id,omitempty"`
	// Batch is set for subscription batch tasks, which have no scan payload.
	Batch *domain.SubscriptionBatch `json:"batch,omitempty"`
}

// ScanPayload is the stored JSON payload of a scan task.
type ScanPayload struct {
	Source        domain.LibrarySource `json:"source"`
	Scan          domain.ScanProgress  `json:"scan"`
	TargetID      string               `json:"target_id,omitempty"`
	TargetPath    string               `json:"target_path,omitempty"`
	TargetFile    bool                 `json:"target_file,omitempty"`
	OfflineTaskID int                  `json:"offline_task_id,omitempty"`
	Code          string               `json:"code,omitempty"`
	JavDBID       string               `json:"javdb_id,omitempty"`
}

type metadataTaskGroup struct {
	ParentID  int         `json:"parent_id"`
	Count     int         `json:"count"`
	Type      string      `json:"type"`
	Status    task.Status `json:"status"`
	Error     *string     `json:"error"`
	UpdatedAt time.Time   `json:"updated_at"`
}

const recentBatchTasks = 5

// ListWorkflows retrieves up to 20 scan tasks and any currently active scans,
// folded with their scrape and artwork child job status, plus recent and
// active subscription batch tasks.
func ListWorkflows(ctx context.Context, database *ent.Client) ([]TaskInfo, error) {
	records, err := database.Task.Query().Where(task.TypeEQ(string(KindScan))).
		Order(ent.Desc(task.FieldID)).Limit(20).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list scan tasks: %w", err)
	}
	active, err := database.Task.Query().Where(task.TypeIn(string(KindScan), string(KindScrape), string(KindCover)),
		task.StatusIn(task.StatusQueued, task.StatusRunning), func(s *sql.Selector) {
			s.Select("CASE WHEN type = '" + string(KindScan) + "' THEN id ELSE " + JSONExtract(task.FieldPayload, PathScanTaskID) + " END").Distinct()
		}).Select(task.FieldID).Ints(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active library tasks: %w", err)
	}
	ids := make(map[int]bool)
	for _, record := range records {
		ids[record.ID] = true
	}
	var missing []int
	for _, id := range active {
		if !ids[id] {
			missing = append(missing, id)
			ids[id] = true
		}
	}
	if len(missing) > 0 {
		parents, err := database.Task.Query().Where(task.IDIn(missing...)).All(ctx)
		if err != nil {
			return nil, err
		}
		records = append(records, parents...)
	}
	result, err := WorkflowInfos(ctx, database, records)
	if err != nil {
		return nil, err
	}
	batches, err := listBatchTasks(ctx, database)
	if err != nil {
		return nil, err
	}
	result = append(result, batches...)
	slices.SortFunc(result, func(a, b TaskInfo) int {
		aActive := a.Status == task.StatusQueued || a.Status == task.StatusRunning
		bActive := b.Status == task.StatusQueued || b.Status == task.StatusRunning
		if aActive != bActive {
			if aActive {
				return -1
			}
			return 1
		}
		if a.Status == task.StatusRunning && b.Status == task.StatusQueued {
			return -1
		}
		if a.Status == task.StatusQueued && b.Status == task.StatusRunning {
			return 1
		}
		return b.ID - a.ID
	})
	return result, nil
}

// WorkflowInfo returns folded status for a single scan task by ID.
func WorkflowInfo(ctx context.Context, database *ent.Client, id int) (TaskInfo, error) {
	record, err := database.Task.Get(ctx, id)
	if err != nil {
		return TaskInfo{}, err
	}
	infos, err := WorkflowInfos(ctx, database, []*ent.Task{record})
	if err != nil {
		return TaskInfo{}, err
	}
	return infos[0], nil
}

// WorkflowInfos folds metadata child tasks into their parent scan workflow summaries.
func WorkflowInfos(ctx context.Context, database *ent.Client, records []*ent.Task) ([]TaskInfo, error) {
	result := make([]TaskInfo, 0, len(records))
	if len(records) == 0 {
		return result, nil
	}
	children, err := metadataGroups(ctx, database, records)
	if err != nil {
		return nil, fmt.Errorf("read metadata workflow progress: %w", err)
	}
	byParent := make(map[int][]metadataTaskGroup)
	for _, child := range children {
		byParent[child.ParentID] = append(byParent[child.ParentID], child)
	}
	for _, record := range records {
		info, err := ScanTaskInfo(record)
		if err != nil {
			return nil, err
		}
		active, running, failed, artwork := false, false, false, false
		for _, child := range byParent[record.ID] {
			if child.Type == string(KindScrape) {
				info.Scan.MetadataTotal += child.Count
			}
			if (child.Type == string(KindCover) && child.Status == task.StatusDone) || child.Status == task.StatusFailed {
				info.Scan.MetadataCompleted += child.Count
			}
			active = active || child.Status == task.StatusQueued || child.Status == task.StatusRunning
			running = running || child.Status == task.StatusRunning
			artwork = artwork || (child.Type == string(KindCover) && child.Status == task.StatusRunning)
			if child.Status == task.StatusFailed {
				failed = true
				if info.Error == nil {
					info.Error = child.Error
				}
			}
			if child.UpdatedAt.After(info.UpdatedAt) {
				info.UpdatedAt = child.UpdatedAt
			}
		}
		if info.Scan.MetadataTotal > 0 && info.Status != task.StatusFailed {
			info.Progress = info.Scan.MetadataCompleted * 100 / info.Scan.MetadataTotal
			if active {
				info.Status, info.Scan.Stage = task.StatusQueued, "scraping"
				if running {
					info.Status = task.StatusRunning
				}
				if artwork {
					info.Scan.Stage = "artwork"
				}
			} else if failed {
				info.Status = task.StatusFailed
			}
		}
		result = append(result, info)
	}
	return result, nil
}

// Return one row per parent/type/status, with its count and latest change.
// Keep large cover documents inside SQLite instead of decoding every child.
func metadataGroups(ctx context.Context, database *ent.Client, records []*ent.Task) ([]metadataTaskGroup, error) {
	children := sql.Table(task.Table)
	parents := make([]any, 0, len(records))
	for _, record := range records {
		parents = append(parents, record.ID)
	}
	parent := JSONExtract(children.C(task.FieldPayload), PathScanTaskID)
	partition := "PARTITION BY " + parent + ", " + children.C(task.FieldType) + ", " + children.C(task.FieldStatus)
	groups := sql.Select(
		children.C(task.FieldID), sql.As(parent, "parent_id"),
		sql.As("COUNT(*) OVER ("+partition+")", "count"),
		sql.As("ROW_NUMBER() OVER ("+partition+" ORDER BY "+children.C(task.FieldUpdatedAt)+" DESC, "+children.C(task.FieldID)+" DESC)", "position"),
	).From(children).Where(sql.And(sql.In(children.C(task.FieldType), string(KindScrape), string(KindCover)),
		sqljson.ValueIn(children.C(task.FieldPayload), parents, sqljson.Path(PathScanTaskID)))).As("metadata_groups")
	var result []metadataTaskGroup
	err := database.Task.Query().Where(func(s *sql.Selector) {
		s.Join(groups).On(s.C(task.FieldID), groups.C(task.FieldID))
		s.Where(sql.EQ(groups.C("position"), 1))
		s.Select(s.C(task.FieldType), s.C(task.FieldStatus), s.C(task.FieldError), s.C(task.FieldUpdatedAt), groups.C("parent_id"), groups.C("count"))
	}).Select(task.FieldID).Scan(ctx, &result)
	return result, err
}

// ScanTaskInfo extracts TaskInfo from a scan task ent.Task record.
func ScanTaskInfo(record *ent.Task) (TaskInfo, error) {
	payload, err := DecodePayload[ScanPayload](record.Payload)
	if err != nil {
		return TaskInfo{}, fmt.Errorf("read scan task %d: %w", record.ID, err)
	}
	return TaskInfo{
		ID: record.ID, Type: record.Type, Status: record.Status, Progress: record.Progress,
		Error: record.Error, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		Source: payload.Source, Scan: payload.Scan, OfflineTaskID: payload.OfflineTaskID,
	}, nil
}

// EnsureScanTask finds an existing reusable scan task for the source or creates a new queued scan.
func EnsureScanTask(ctx context.Context, tasks *ent.TaskClient, source domain.LibrarySource, reusable ...task.Status) (*ent.Task, error) {
	record, err := tasks.Query().Where(
		task.TypeEQ(string(KindScan)), task.StatusIn(reusable...),
		func(selector *sql.Selector) {
			selector.Where(sql.And(
				sql.Not(sqljson.HasKey(task.FieldPayload, sqljson.Path(PathTargetID))),
				sqljson.ValueEQ(task.FieldPayload, source.AccountID, sqljson.Path(PathSource, PathAccountID)),
				sqljson.ValueEQ(task.FieldPayload, source.Directory.ID, sqljson.Path(PathSource, "directory", "id")),
			))
		},
	).First(ctx)
	if err == nil {
		return record, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("find active scan: %w", err)
	}
	payload, err := EncodePayload(ScanPayload{
		Source: source, Scan: domain.ScanProgress{Stage: "queued", CurrentPath: source.Directory.Path},
	})
	if err != nil {
		return nil, err
	}
	record, err = tasks.Create().SetType(string(KindScan)).SetPayload(payload).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("queue library scan: %w", err)
	}
	return record, nil
}
