package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/tasks"
)

func TestTaskGroupsFoldChildCountsAndLatestState(t *testing.T) {
	library, parent, payload := libraryFixture(t)
	ctx := t.Context()
	if err := library.Tasks().Queue().Finish(ctx, parent.ID, nil); err != nil {
		t.Fatal(err)
	}
	var activeID int
	latest := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, child := range []struct {
		kind   string
		status task.Status
	}{
		{"scrape", task.StatusDone}, {"scrape", task.StatusDone}, {"scrape", task.StatusFailed},
		{"cover", task.StatusDone}, {"cover", task.StatusRunning},
	} {
		input, err := tasks.EncodePayload(scrape.MetadataPayload{Source: payload.Source, ScanTaskID: parent.ID})
		if err != nil {
			t.Fatal(err)
		}
		input, err = tasks.SetPayloadField(input, "document", strings.Repeat("fixture document ", 1000))
		if err != nil {
			t.Fatal(err)
		}
		builder := library.Database().Task.Create().SetType(child.kind).SetStatus(child.status).
			SetPayload(input).SetUpdatedAt(latest.Add(time.Duration(i) * time.Second))
		if child.status == task.StatusFailed {
			builder.SetError("fixture metadata failure")
		}
		record, err := builder.Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		activeID = record.ID
	}
	info, err := library.Tasks().Info(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != task.StatusRunning || info.Scan.Stage != "artwork" || info.Scan.MetadataTotal != 3 || info.Scan.MetadataCompleted != 2 || info.Progress != 66 || info.Error == nil {
		t.Fatalf("workflow: %+v", info)
	}
	if !info.UpdatedAt.Equal(latest.Add(4 * time.Second)) {
		t.Fatalf("latest change: %v", info.UpdatedAt)
	}
	if err := library.Tasks().Queue().Finish(ctx, activeID, errors.New("fixture cover failure")); err != nil {
		t.Fatal(err)
	}
	info, err = library.Tasks().Info(ctx, parent.ID)
	if err != nil || info.Status != task.StatusFailed || info.Scan.MetadataCompleted != 3 || info.Progress != 100 {
		t.Fatalf("finished workflow: %+v err=%v", info, err)
	}
}

func TestTaskListRetainsOlderActiveWorkflows(t *testing.T) {
	library, parent, payload := libraryFixture(t)
	ctx := t.Context()
	if err := library.Tasks().Queue().Finish(ctx, parent.ID, nil); err != nil {
		t.Fatal(err)
	}
	input, err := tasks.EncodePayload(scrape.MetadataPayload{Source: payload.Source, ScanTaskID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := library.Database().Task.Create().SetType("scrape").SetPayload(input).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	encoded, err := tasks.EncodePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	for range 21 {
		if err := library.Database().Task.Create().SetType("scan").SetStatus(task.StatusDone).SetPayload(encoded).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	items, err := library.Tasks().List(ctx)
	if err != nil || len(items) != 21 || items[0].ID != parent.ID || items[0].Status != task.StatusQueued {
		t.Fatalf("active workflows: %+v err=%v", items, err)
	}
}
