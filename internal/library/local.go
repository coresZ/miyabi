package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/library/scan"
	"github.com/ppxb/miyabi/internal/localsource"
)

const embyLocalDirectoryName = "Emby 本地目录"

// startLocalScan queues an import of the configured Emby directory.
func (s *Service) startLocalScan(ctx context.Context) (domain.TaskInfo, error) {
	root, err := s.localScanRoot()
	if err != nil {
		return domain.TaskInfo{}, err
	}
	return s.EnqueueScanMode(ctx, domain.LibrarySource{
		AccountID: domain.LocalAccountID,
		Directory: domain.LibraryDirectory{ID: root, Name: embyLocalDirectoryName, Path: root},
	}, true)
}

// StartLocalScan queues a scan of one configured local media directory.
func (s *Service) StartLocalScan(ctx context.Context, id string) (domain.TaskInfo, error) {
	if s.localSourceStore == nil {
		return domain.TaskInfo{}, domain.E(domain.KindNotFound, "本地媒体目录不存在", nil)
	}
	source, found, err := s.localSourceStore.Find(ctx, id)
	if err != nil {
		return domain.TaskInfo{}, err
	}
	if !found || !source.Enabled {
		return domain.TaskInfo{}, domain.E(domain.KindNotFound, "本地媒体目录不存在或已停用", nil)
	}
	return s.EnqueueScan(ctx, localLibrarySource(source))
}

// localLibrarySource identifies a configured directory in scan payloads, so
// queued scans, task listings and per-source rescans share one identity.
func localLibrarySource(source localsource.Source) domain.LibrarySource {
	return domain.LibrarySource{
		AccountID: domain.LocalAccountID,
		Directory: domain.LibraryDirectory{ID: source.ID, Name: source.Name, Path: source.Path},
	}
}

// ScheduleLocalScan also runs at startup, where a fresh install may not have
// created its export directory yet. No media needs importing in that case.
func (s *Service) ScheduleLocalScan(ctx context.Context) error {
	if s.exportMgr.Config().EmbyDir == "" {
		return nil
	}
	_, err := s.startLocalScan(ctx)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// StartLocalSourceScan queues an import of any user-configured local directory,
// keeping only local metadata by default (Emby exports already carry sidecars).
func (s *Service) StartLocalSourceScan(ctx context.Context, root string) (domain.TaskInfo, error) {
	return s.StartLocalSourceScanMode(ctx, root, true)
}

// StartLocalSourceScanMode queues a local scan that either auto-completes
// metadata or reads only local data.
func (s *Service) StartLocalSourceScanMode(ctx context.Context, root string, metadataOnly bool) (domain.TaskInfo, error) {
	return s.startLocalSourceScan(ctx, root, metadataOnly, false)
}

// StartLocalSourceScanModeIncremental only completes movies that still lack metadata.
func (s *Service) StartLocalSourceScanModeIncremental(ctx context.Context, root string, metadataOnly bool) (domain.TaskInfo, error) {
	return s.startLocalSourceScan(ctx, root, metadataOnly, true)
}

func (s *Service) startLocalSourceScan(ctx context.Context, root string, metadataOnly, incremental bool) (domain.TaskInfo, error) {
	canonical, err := canonicalDirectory(root)
	if err != nil {
		return domain.TaskInfo{}, err
	}
	source := domain.LibrarySource{
		AccountID: domain.LocalAccountID,
		Directory: domain.LibraryDirectory{ID: canonical, Name: "本地目录", Path: canonical},
	}
	if incremental {
		return s.EnqueueSyncMode(ctx, source, metadataOnly)
	}
	return s.EnqueueScanMode(ctx, source, metadataOnly)
}

func (s *Service) localScanRoot() (string, error) {
	dir := s.exportMgr.Config().EmbyDir
	if dir == "" {
		return "", domain.E(domain.KindInvalid, "未配置 Emby 本地目录", nil)
	}
	return canonicalDirectory(dir)
}

// resolveLocalDirectory returns the directory a queued local scan must read. A
// scan whose source was removed or reconfigured must not read the old path.
func (s *Service) resolveLocalDirectory(ctx context.Context, queued domain.LibrarySource) (string, error) {
	if root, err := s.localScanRoot(); err == nil && root == queued.Directory.ID {
		return root, nil
	}
	stale := domain.E(domain.KindConflict, "本地媒体目录已变更，请重新扫描", nil)
	if s.localSourceStore == nil {
		return "", stale
	}
	source, found, err := s.localSourceStore.Find(ctx, queued.Directory.ID)
	if err != nil {
		return "", err
	}
	if !found || !source.Enabled || source.Path != queued.Directory.Path {
		return "", stale
	}
	return source.Path, nil
}

func canonicalDirectory(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func (s *Service) scanLocal(ctx context.Context, id int, payload domain.ScanPayload) error {
	root, err := s.resolveLocalDirectory(ctx, payload.Source)
	if err != nil {
		return err
	}
	payload.Scan = domain.ScanProgress{Stage: "scanning", CurrentPath: root}
	if err := scan.ReportScan(ctx, s.database.Task, id, payload, s.tasks); err != nil {
		return err
	}
	result, err := s.localScanner.Scan(ctx, root)
	if err != nil {
		return fmt.Errorf("scan local directory %s: %w", root, err)
	}
	// Local files without an NFO still need central metadata scraping; a
	// metadata-only scan settles them instead.
	queued, err := scan.EnqueueLocalScrapes(ctx, s.database, id, payload.Source, result.PendingMovieIDs, payload.MetadataOnly)
	if err != nil {
		return err
	}
	if queued > 0 {
		s.tasks.WakePool()
	}
	payload.Scan.Stage = "done"
	payload.Scan.FilesScanned = result.FilesScanned
	payload.Scan.VideoFiles = result.MediaFiles
	payload.Scan.MatchedFiles = result.MediaFiles
	payload.Scan.Movies = result.MoviesAdded
	// Metadata progress folds the queued scrape children into this scan task.
	return scan.ReportScan(ctx, s.database.Task, id, payload, s.tasks)
}
