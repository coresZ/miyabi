package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestPanDatabaseCommitDoesNotBlockPlaybackOrCanceledWaiters(t *testing.T) {
	play, source := playFixture(t)
	drive := play.drive
	playback, err := play.createSession(source, authorizationVersion(t, drive), []pan.PlaySource{{URL: "https://cdn.example/video", Height: 1080}})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	hold, release := panTestGate(t)
	committed := make(chan error, 1)
	go func() {
		sess, err := drive.OpenSource(t.Context(), source)
		if err != nil {
			committed <- err
			return
		}
		committed <- sess.Commit(t.Context(), func(tx *ent.Tx) error {
			started <- struct{}{}
			<-hold
			return nil
		})
	}()
	awaitPan(t, started)
	checked := make(chan error, 1)
	go func() {
		_, _, err := play.resource(playback.ID, 0)
		checked <- err
	}()
	if err := awaitPan(t, checked); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	changed := make(chan error, 1)
	go func() { changed <- drive.ClearDirectory(ctx) }()
	cancel()
	if err := awaitPan(t, changed); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled commit waiter = %v", err)
	}
	release()
	if err := awaitPan(t, committed); err != nil {
		t.Fatal(err)
	}
}
