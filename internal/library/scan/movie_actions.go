package scan

import (
	"context"
	"fmt"
	"path/filepath"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/tasks"
)

// sourceLock runs fn while the source that owns a movie cannot change. A local
// root needs no such guard, so it runs fn directly.
type sourceLock func(context.Context, func() error) error

// RescrapeMovie queues a fresh metadata workflow for the indexed files of one movie.
// A supplied code persists the user's correction before the recoverable job starts.
func (s *Scanner) RescrapeMovie(ctx context.Context, id int, code string) (*ent.Task, error) {
	if code != "" && !codeid.Valid(code) {
		return nil, domain.E(domain.KindInvalid, "请输入有效的影片番号", nil)
	}
	code = codeid.Normalize(code)
	source, withSource, err := s.rescrapeTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	var parent *ent.Task
	// Mount callbacks enqueue with the source lock held. Keep the same order,
	// and acquire the queue before opening a database write transaction.
	err = withSource(ctx, func() error {
		if err := s.tasksSvc.Queue().Lock(ctx); err != nil {
			return err
		}
		defer s.tasksSvc.Queue().Unlock()
		return ent.WithTx(ctx, s.db, func(tx *ent.Tx) error {
			record, err := tx.Movie.Query().Where(movie.IDEQ(id), movie.HasFilesWith(scrape.FileScope(source))).Only(ctx)
			if ent.IsNotFound(err) {
				return domain.E(domain.KindNotFound, "媒体库中未找到该影片的媒体文件", nil)
			}
			if err != nil {
				return err
			}
			correcting := code != "" && code != record.Code
			active, err := tx.Task.Query().Where(task.TypeEQ(tasks.KindScrape.String()),
				task.ResourceKeyEQ(fmt.Sprintf("movie:%d", id)), task.StatusIn(task.StatusQueued, task.StatusRunning)).First(ctx)
			if err != nil && !ent.IsNotFound(err) {
				return err
			}
			if active != nil {
				input, err := tasks.DecodePayload[scrape.MetadataPayload](active.Payload)
				if err != nil {
					return err
				}
				if !correcting && input.Rebuild && input.Source == source {
					parent, err = tx.Task.Get(ctx, input.ScanTaskID)
					return err
				}
				return domain.E(domain.KindConflict, "该影片正在处理中，请完成后再操作", nil)
			}
			busy, err := tx.Task.Query().Where(task.TypeEQ(tasks.KindScan.String()),
				task.StatusIn(task.StatusQueued, task.StatusRunning), func(q *sql.Selector) {
					q.Where(sqljson.ValueEQ(task.FieldPayload, source.AccountID, sqljson.Path("source", "account_id")))
					q.Where(sqljson.ValueEQ(task.FieldPayload, source.Directory.ID, sqljson.Path("source", "directory", "id")))
				}).Exist(ctx)
			if err != nil {
				return err
			}
			if busy {
				return domain.E(domain.KindConflict, "媒体库正在扫描，请完成后再操作", nil)
			}
			if correcting {
				matches, err := MatchMovies(ctx, tx, []string{code})
				if err != nil {
					return err
				}
				if other := matches[code]; other != 0 && other != id {
					return domain.E(domain.KindConflict, "该番号已存在于媒体库，请检查后重试", nil)
				}
				if err := scrape.SaveMovieMetadata(ctx, tx, id, nfo.Movie{Code: code}); err != nil {
					return err
				}
				update := tx.Movie.UpdateOneID(id).SetManualCode(code).ClearCover().ClearPoster().SetFanarts([]string{})
				if record.MetadataSnapshot != nil {
					snapshot := *record.MetadataSnapshot
					if snapshot.Code == "" {
						snapshot.Code = record.Code
					}
					update.SetMetadataSnapshot(&snapshot)
				}
				record, err = update.Save(ctx)
				if err != nil {
					return err
				}
			} else if err := tx.Movie.UpdateOneID(id).SetScrapeStatus(movie.ScrapeStatusPending).Exec(ctx); err != nil {
				return err
			}
			body, err := tasks.EncodePayload(domain.ScanPayload{
				MovieID: id, Code: record.Code, Rebuild: true, Source: source,
				Scan: domain.ScanProgress{Stage: "done", Movies: 1, CurrentPath: source.Directory.Path},
			})
			if err != nil {
				return err
			}
			parent, err = tx.Task.Create().SetType(tasks.KindScan.String()).SetStatus(task.StatusDone).SetPayload(body).Save(ctx)
			if err != nil {
				return err
			}
			body, err = tasks.EncodePayload(scrape.MetadataPayload{
				Rebuild: true, Source: source, ScanTaskID: parent.ID, MovieID: id,
				Code: record.Code, JavDBID: domain.ValueOrZero(record.JavdbID), ManualCode: record.ManualCode,
			})
			if err != nil {
				return err
			}
			return tx.Task.Create().SetType(tasks.KindScrape.String()).SetResourceKey(fmt.Sprintf("movie:%d", id)).SetPayload(body).Exec(ctx)
		})
	})
	if err != nil {
		return nil, err
	}
	s.tasksSvc.NotifyLibraryChanged()
	s.tasksSvc.WakePool()
	return parent, nil
}

// rescrapeTarget resolves the media source that owns a movie and the lock that
// keeps it from changing. A mounted 115 source wins when it indexes the movie;
// otherwise the local media root recorded on its files is used.
func (s *Scanner) rescrapeTarget(ctx context.Context, id int) (domain.LibrarySource, sourceLock, error) {
	if s.driveSvc != nil {
		if sess, err := s.driveSvc.Open(ctx); err == nil {
			exists, queryErr := s.db.Movie.Query().Where(movie.IDEQ(id),
				movie.HasFilesWith(database.LibraryFiles(sess.Source()))).Exist(ctx)
			if queryErr != nil {
				return domain.LibrarySource{}, nil, queryErr
			}
			if exists {
				return sess.Source(), sess.WithSource, nil
			}
		}
	}
	source, found, err := s.localMovieSource(ctx, id)
	if err != nil {
		return domain.LibrarySource{}, nil, err
	}
	if found {
		return source, func(_ context.Context, fn func() error) error { return fn() }, nil
	}
	return domain.LibrarySource{}, nil, domain.E(domain.KindNotFound, "媒体库中未找到该影片的媒体文件", nil)
}

// localMovieSource builds the local source from the root recorded on a movie's
// local files.
func (s *Scanner) localMovieSource(ctx context.Context, id int) (domain.LibrarySource, bool, error) {
	record, err := s.db.File.Query().Where(file.MovieIDEQ(id), file.AccountIDEQ(domain.LocalAccountID)).
		Select(file.FieldRootID).Order(ent.Asc(file.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return domain.LibrarySource{}, false, nil
	}
	if err != nil {
		return domain.LibrarySource{}, false, err
	}
	root := record.RootID
	if root == "" {
		return domain.LibrarySource{}, false, domain.E(domain.KindInvalid, "本地视频缺少有效的目录信息", nil)
	}
	return domain.LibrarySource{
		AccountID: domain.LocalAccountID,
		Directory: domain.LibraryDirectory{ID: root, Name: filepath.Base(root), Path: root},
	}, true, nil
}
