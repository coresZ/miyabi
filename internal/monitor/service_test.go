package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent/subscription"
	"github.com/ppxb/miyabi/internal/magnet"
	"github.com/ppxb/miyabi/internal/tasks"
)

type mockDiscoverer struct {
	mu           sync.Mutex
	summaries    map[string]domain.MovieSummary
	magnets      map[string][]domain.Magnet
	actorMovies  map[string][]domain.Movie
	summaryCalls int
	magnetsCalls int
	browseCalls  int
}

func (m *mockDiscoverer) MovieSummary(ctx context.Context, movieID string) (domain.MovieSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summaryCalls++
	if s, ok := m.summaries[movieID]; ok {
		return s, nil
	}
	return domain.MovieSummary{
		ID:          movieID,
		Code:        "MOCK-" + movieID,
		Title:       "Mock Title " + movieID,
		Cover:       "https://example.com/cover.jpg",
		ReleaseDate: "2026-10-01",
	}, nil
}

func (m *mockDiscoverer) CatalogueMagnets(ctx context.Context, movieID string) ([]domain.Magnet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.magnetsCalls++
	return m.magnets[movieID], nil
}

func (m *mockDiscoverer) BrowseMovies(ctx context.Context, options domain.BrowseOptions) ([]domain.Movie, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.browseCalls++
	return m.actorMovies[options.EntityID], nil
}

type mockOfflineAdder struct {
	mu          sync.Mutex
	submissions []struct {
		MovieID string
		Hash    string
	}
}

func (m *mockOfflineAdder) Add(ctx context.Context, movieID string, hash string) (domain.OfflineSubmission, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.submissions = append(m.submissions, struct {
		MovieID string
		Hash    string
	}{MovieID: movieID, Hash: hash})
	return domain.OfflineSubmission{
		TaskID:  len(m.submissions),
		Code:    movieID,
		JavDBID: movieID,
		Hash:    hash,
		Status:  "running",
	}, nil
}

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

func TestMovieSubscriptionLifecycle(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	disc := &mockDiscoverer{
		summaries: make(map[string]domain.MovieSummary),
		magnets:   make(map[string][]domain.Magnet),
	}
	offline := &mockOfflineAdder{}
	taskSvc := tasks.NewService(store.Client, tasks.NewRegistry())
	service := New(store.Client, disc, offline, taskSvc)
	ctx := t.Context()

	// 1. Add movie subscription
	item, err := service.AddMovie(ctx, "m1", AddMovieOptions{})
	if err != nil {
		t.Fatalf("AddMovie failed: %v", err)
	}
	if item.Code != "MOCK-m1" || item.Status != StatusWaiting {
		t.Fatalf("unexpected item: %#v", item)
	}

	// 2. List subscriptions
	list, err := service.List(ctx, "movie", 1, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 item, got %d (err: %v)", len(list), err)
	}

	// 3. Update subscription
	newAuto := false
	zone := "censored"
	updated, err := service.Update(ctx, item.ID, UpdateOptions{
		AutoDownload: &newAuto,
		Zone:         &zone,
	})
	if err != nil || updated.AutoDownload != false || updated.Zone != "censored" {
		t.Fatalf("update failed: %#v (err: %v)", updated, err)
	}

	// 4. Retry subscription
	retried, err := service.Retry(ctx, item.ID)
	if err != nil || retried.Status != StatusWaiting {
		t.Fatalf("retry failed: %#v (err: %v)", retried, err)
	}

	// 5. Remove subscription
	if err := service.Remove(ctx, item.ID); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if err := service.Remove(ctx, item.ID); err == nil {
		t.Fatal("removing already removed subscription should fail")
	}
}

func TestEnqueueSingle(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	disc := &mockDiscoverer{
		summaries: make(map[string]domain.MovieSummary),
		magnets:   make(map[string][]domain.Magnet),
	}
	offline := &mockOfflineAdder{}
	taskSvc := tasks.NewService(store.Client, tasks.NewRegistry())
	service := New(store.Client, disc, offline, taskSvc)
	ctx := t.Context()

	item, err := service.AddMovie(ctx, "m1", AddMovieOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Case A: No magnets available -> keeps waiting and enables auto_download
	enqueued, err := service.EnqueueSingle(ctx, item.ID)
	if err != nil {
		t.Fatalf("enqueue single failed: %v", err)
	}
	if enqueued.Status != StatusWaiting || !enqueued.AutoDownload {
		t.Fatalf("expected waiting with auto_download=true, got %#v", enqueued)
	}

	// Case B: Magnet available -> picked and submitted to 115
	disc.magnets["m1"] = []domain.Magnet{
		{Hash: "abc123hash", Name: "MOCK-m1 With Sub", HasSubtitle: true, HD: true, Size: 1024 * 1024 * 1024},
	}

	enqueued, err = service.EnqueueSingle(ctx, item.ID)
	if err != nil {
		t.Fatalf("enqueue single with magnet failed: %v", err)
	}
	if enqueued.Status != StatusAdded || enqueued.Hash != "abc123hash" || enqueued.TaskID == nil {
		t.Fatalf("expected added with task_id, got %#v", enqueued)
	}
	if len(offline.submissions) != 1 || offline.submissions[0].Hash != "abc123hash" {
		t.Fatalf("offline submission mismatch: %#v", offline.submissions)
	}
}

func TestActorSubscriptionAndFeed(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	disc := &mockDiscoverer{
		summaries:   make(map[string]domain.MovieSummary),
		magnets:     make(map[string][]domain.Magnet),
		actorMovies: make(map[string][]domain.Movie),
	}
	offline := &mockOfflineAdder{}
	taskSvc := tasks.NewService(store.Client, tasks.NewRegistry())
	service := New(store.Client, disc, offline, taskSvc)
	ctx := t.Context()

	// Setup mock actor works: 1 historical movie, 1 upcoming unreleased movie
	disc.actorMovies["actor-1"] = []domain.Movie{
		{
			ID:          "act-upcoming",
			Code:        "UP-001",
			Title:       "Upcoming Movie",
			ReleaseDate: "2099-01-01",
			Actors: []domain.Actor{
				{ID: "actor-1", Name: "Yua Mikami", Avatar: "https://example.com/avatar.jpg"},
			},
		},
		{
			ID:          "act-past",
			Code:        "PAST-001",
			Title:       "Past Movie",
			ReleaseDate: "2020-01-01",
			Actors: []domain.Actor{
				{ID: "actor-1", Name: "Yua Mikami", Avatar: "https://example.com/avatar.jpg"},
			},
		},
	}

	// 1. Add Actor
	actorSub, err := service.AddActor(ctx, "actor-1", AddActorOptions{})
	if err != nil {
		t.Fatalf("AddActor failed: %v", err)
	}
	if actorSub.Title != "Yua Mikami" || actorSub.Cover != "https://example.com/avatar.jpg" {
		t.Fatalf("actor details not extracted: %#v", actorSub)
	}

	// The upcoming movie should automatically have a movie subscription spawned!
	feed, err := service.ActorFeed(ctx, actorSub.ID, 1, 10)
	if err != nil {
		t.Fatalf("ActorFeed failed: %v", err)
	}
	if len(feed) != 1 || feed[0].TargetID != "act-upcoming" {
		t.Fatalf("expected 1 spawned upcoming movie, got: %#v", feed)
	}

	// 2. Simulate new release discovered via Check
	disc.actorMovies["actor-1"] = append([]domain.Movie{
		{
			ID:          "act-brand-new",
			Code:        "NEW-001",
			Title:       "Brand New Release",
			ReleaseDate: "2099-02-01",
		},
	}, disc.actorMovies["actor-1"]...)

	// Force actor subscription next_check_at to past so Check runs it
	_, err = store.Client.Subscription.UpdateOneID(actorSub.ID).
		SetNextCheckAt(time.Now().Add(-time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Check(ctx); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Verify feed now has 2 movies (act-brand-new and act-upcoming)
	feed, err = service.ActorFeed(ctx, actorSub.ID, 1, 10)
	if err != nil || len(feed) != 2 {
		t.Fatalf("expected 2 movies in feed, got: %d (%v)", len(feed), err)
	}
}

func TestBatchEnqueueTask(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	disc := &mockDiscoverer{
		summaries: make(map[string]domain.MovieSummary),
		magnets:   make(map[string][]domain.Magnet),
	}
	offline := &mockOfflineAdder{}
	taskSvc := tasks.NewService(store.Client, tasks.NewRegistry())
	service := New(store.Client, disc, offline, taskSvc)
	ctx := t.Context()

	item1, _ := service.AddMovie(ctx, "batch-1", AddMovieOptions{})
	item2, _ := service.AddMovie(ctx, "batch-2", AddMovieOptions{})

	disc.magnets["batch-1"] = []domain.Magnet{
		{Hash: "hash1", Name: "batch-1", HasSubtitle: true, HD: true, Size: 1000},
	}

	// Enqueue batch
	taskID, err := service.EnqueueBatch(ctx, BatchEnqueueRequest{
		IDs: []int{item1.ID, item2.ID},
	})
	if err != nil {
		t.Fatalf("EnqueueBatch failed: %v", err)
	}
	if taskID <= 0 {
		t.Fatalf("expected positive taskID, got %d", taskID)
	}

	taskRow, err := store.Client.Task.Get(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}

	// Execute batch task directly
	job := tasks.Job{
		ID:      taskRow.ID,
		Type:    tasks.KindSubscriptionBatch,
		Payload: taskRow.Payload,
	}
	if err := service.BatchHandler(ctx, job); err != nil {
		t.Fatalf("BatchHandler failed: %v", err)
	}

	// Verify item1 became added (since it had magnet)
	sub1, _ := store.Client.Subscription.Get(ctx, item1.ID)
	if sub1.Status != subscription.StatusAdded || sub1.Hash != "hash1" {
		t.Fatalf("expected sub1 added with hash1, got status=%s hash=%s", sub1.Status, sub1.Hash)
	}

	// Verify item2 stayed waiting with auto_download=true (since no magnet yet)
	sub2, _ := store.Client.Subscription.Get(ctx, item2.ID)
	if sub2.Status != subscription.StatusWaiting || !sub2.AutoDownload {
		t.Fatalf("expected sub2 waiting with auto_download=true, got status=%s auto_download=%v", sub2.Status, sub2.AutoDownload)
	}
}

func TestSubscriptionSettings(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	taskSvc := tasks.NewService(store.Client, tasks.NewRegistry())
	service := New(store.Client, nil, nil, taskSvc)
	ctx := t.Context()

	cfg, err := service.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MovieAutoDownload || cfg.ActorAutoDownload {
		t.Fatalf("default config mismatch: %#v", cfg)
	}

	cfg.ActorAutoDownload = true
	cfg.ActorCheckTime = "05:30"
	cfg.Preferences.Subtitle = magnet.PreferenceRequired
	if err := service.UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	updated, err := service.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.ActorAutoDownload || updated.ActorCheckTime != "05:30" || updated.Preferences.Subtitle != magnet.PreferenceRequired {
		t.Fatalf("updated config mismatch: %#v", updated)
	}
}
