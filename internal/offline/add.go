package offline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/tasks"
)

// Add validates and records a new offline download for the given movie ID and magnet hash.
func (service *Service) Add(ctx context.Context, movieID, hash string) (Submission, error) {
	hash = strings.ToLower(hash)
	if service.catalogue != nil {
		has, err := service.catalogue.HasMagnet(ctx, movieID, hash)
		if err != nil {
			return Submission{}, err
		}
		if !has {
			return Submission{}, ErrMagnetNotFound
		}
	}
	var rawCode string
	if service.catalogue != nil {
		var err error
		rawCode, err = service.catalogue.MovieCode(ctx, movieID)
		if err != nil {
			return Submission{}, err
		}
	}
	code := codeid.Normalize(rawCode)

	sess, err := service.drive.Open(ctx)
	if err != nil {
		return Submission{}, fmt.Errorf("get 115 account for offline download: %w", err)
	}
	source := sess.Source()
	directory := source.Directory

	unlock, err := service.operations.Lock(ctx, source.AccountID, hash)
	if err != nil {
		return Submission{}, err
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
			return Submission{}, err
		}
		if input.DirectoryID != directory.ID {
			return Submission{}, domain.E(domain.KindConflict, "该磁力正在下载到另一个目录，请先在 115 中处理该任务", nil)
		}
		return service.submission(ctx, existing, &source)
	}
	if !ent.IsNotFound(err) {
		return Submission{}, fmt.Errorf("find active offline task: %w", err)
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
			return Submission{}, err
		}
		if state.Processing {
			return state, nil
		}
	} else if !ent.IsNotFound(err) {
		return Submission{}, fmt.Errorf("find download workflow: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return Submission{}, err
	}
	done, ok := service.drive.StartWork()
	if !ok {
		return Submission{}, context.Canceled
	}
	defer done()

	// Once a remote mutation starts, finish recording it even if the tab closes.
	submitContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	remote, err := service.submit(submitContext, sess, hash)
	if err != nil {
		return Submission{}, fmt.Errorf("submit 115 offline download: %w", err)
	}
	input := offlinePayload{
		Code:        code,
		JavDBID:     movieID,
		Hash:        hash,
		InfoHash:    remote.Hash,
		AccountID:   source.AccountID,
		DirectoryID: directory.ID,
	}
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		return Submission{}, err
	}

	var created *ent.Task
	if err := service.drive.Commit(submitContext, func(tx *ent.Tx) error {
		var err error
		created, err = tx.Task.Create().
			SetType(tasks.KindOffline.String()).
			SetStatus(task.StatusRunning).
			SetPayload(encoded).
			Save(submitContext)
		if err != nil {
			return err
		}
		if remote.Status == 2 {
			return service.completeTask(submitContext, tx, created, input, remote.FileID, sess)
		}
		return nil
	}); err != nil {
		return Submission{}, fmt.Errorf("record 115 offline download: %w", err)
	}

	service.tasks.NotifyOfflineChanged()
	created, err = service.database.Task.Get(submitContext, created.ID)
	if err != nil {
		return Submission{}, err
	}
	return service.submission(submitContext, created, &source)
}
