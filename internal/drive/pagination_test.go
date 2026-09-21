package drive

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ppxb/miyabi/internal/pan"
)

func TestFilePaginationRejectsIncompletePagesBeforeVisitingThem(t *testing.T) {
	first := pan.FilePage{Total: 2, HasMore: true, Files: []pan.File{{ID: "first"}}}
	for _, last := range []pan.FilePage{
		{Total: 3, Files: []pan.File{{ID: "last"}}},
		{Total: 2, HasMore: true},
		{Total: 2},
		{Total: 2, Files: []pan.File{{ID: "last"}, {ID: "extra"}}},
	} {
		t.Run(fmt.Sprintf("total_%d_count_%d_more_%t", last.Total, len(last.Files), last.HasMore), func(t *testing.T) {
			visited, fetched := 0, 0
			err := WalkFilePages(t.Context(), func(offset int) (pan.FilePage, error) {
				fetched++
				if offset == 0 {
					return first, nil
				}
				return last, nil
			}, func(pan.FilePage) (bool, error) { visited++; return true, nil })
			if !errors.Is(err, ErrDirectoryIncomplete) || visited != 1 || fetched != 2 {
				t.Fatalf("incomplete page was visited: fetched=%d visited=%d err=%v", fetched, visited, err)
			}
		})
	}
}

func TestFilePaginationStopsForResultsCancellationAndVisitorErrors(t *testing.T) {
	for _, action := range []string{"complete", "found", "cancel", "error"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("cannot persist scanned page")
			fetched := 0
			err := WalkFilePages(ctx, func(offset int) (pan.FilePage, error) {
				fetched++
				if offset != fetched-1 {
					t.Errorf("page offset=%d, want %d", offset, fetched-1)
				}
				return pan.FilePage{Total: 2, HasMore: offset == 0, Files: []pan.File{{ID: fmt.Sprint(offset)}}}, nil
			}, func(pan.FilePage) (bool, error) {
				switch action {
				case "found":
					return false, nil
				case "cancel":
					cancel()
				case "error":
					return false, failure
				}
				return true, nil
			})
			var want error
			wantPages := 1
			switch action {
			case "complete":
				wantPages = 2
			case "cancel":
				want = context.Canceled
			case "error":
				want = failure
			}
			if !errors.Is(err, want) || fetched != wantPages {
				t.Fatalf("fetches=%d err=%v, want %d and %v", fetched, err, wantPages, want)
			}
		})
	}
}

func TestOfflinePaginationStopsWhenFoundOrExhausted(t *testing.T) {
	for _, action := range []string{"exhausted", "found", "error"} {
		t.Run(action, func(t *testing.T) {
			failure := errors.New("cannot read offline page")
			fetched := 0
			err := WalkOfflinePages(t.Context(), func(page int) (pan.OfflinePage, error) {
				fetched++
				if page != fetched {
					t.Errorf("page=%d, want %d", page, fetched)
				}
				if action == "error" && page == 2 {
					return pan.OfflinePage{}, failure
				}
				return pan.OfflinePage{PageCount: 3, Tasks: []pan.OfflineTask{{Hash: fmt.Sprint(page)}}}, nil
			}, func(remote pan.OfflinePage) (bool, error) {
				return !(action == "found" && remote.Tasks[0].Hash == "2"), nil
			})
			wantPages, want := 3, error(nil)
			switch action {
			case "found":
				wantPages = 2
			case "error":
				wantPages, want = 2, failure
			}
			if !errors.Is(err, want) || fetched != wantPages {
				t.Fatalf("fetches=%d err=%v, want %d and %v", fetched, err, wantPages, want)
			}
		})
	}
}
