package monitor

import (
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/ent/subscription"
	"github.com/ppxb/miyabi/internal/tasks"
)

func TestNextMonitorCheckPolicy(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 30, 0, 0, time.Local)
	for _, test := range []struct {
		name    string
		release string
		want    time.Time
		stale   bool
	}{
		{"far before release polls every three days", "2026-10-30", now.Add(72 * time.Hour), false},
		{"just before release polls on release day", "2026-09-20", time.Date(2026, 9, 20, 10, 30, 0, 0, time.Local), false},
		{"release day polls daily", "2026-09-18", now.Add(24 * time.Hour), false},
		{"day 30 after release still polls", "2026-08-19", now.Add(24 * time.Hour), false},
		{"day 31 after release is stale", "2026-08-18", time.Time{}, true},
		{"missing date counts from creation", "", now.Add(24 * time.Hour), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			next, stale := nextMonitorCheck(now, test.release, now.AddDate(0, 0, -3))
			if stale != test.stale || !next.Equal(test.want) {
				t.Fatalf("next=%v stale=%v, want next=%v stale=%v", next, stale, test.want, test.stale)
			}
		})
	}
}

func TestMonitorSchedulesRetryAndStaleTransitions(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	taskSvc := tasks.NewService(store.Client, tasks.NewRegistry())
	service := New(store.Client, nil, nil, taskSvc)
	ctx := t.Context()
	record := store.Client.Subscription.Create().
		SetKind(subscription.KindMovie).
		SetTargetID("m1").
		SetCode("ABC-001").
		SetReleaseDate("2000-01-01").
		SetNextCheckAt(time.Now()).
		SaveX(ctx)
	revision := taskSvc.Revisions().Monitor
	if err := service.deferCheck(ctx, record, time.Now(), errTest); err == nil {
		t.Fatal("deferCheck should surface the cause")
	}
	deferred := store.Client.Subscription.GetX(ctx, record.ID)
	if deferred.Error == nil || deferred.NextCheckAt == nil || deferred.Status != subscription.StatusWaiting {
		t.Fatalf("deferred monitor: %#v", deferred)
	}
	if taskSvc.Revisions().Monitor == revision {
		t.Fatal("monitor changes must notify subscribers")
	}
	retried, err := service.Retry(ctx, "m1")
	if err != nil || retried.Error != nil || retried.Status != StatusWaiting {
		t.Fatalf("retry: %#v %v", retried, err)
	}
	if err := service.Remove(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	if err := service.Remove(ctx, "m1"); err == nil {
		t.Fatal("removing a missing monitor must fail")
	}
}

type testError struct{}

func (testError) Error() string { return "fixture failure" }

var errTest error = testError{}
