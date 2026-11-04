package library

import (
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

func TestPlayablesListsLocalVideosAndPaginates(t *testing.T) {
	lib, root := localPlayFixture(t)
	ctx := t.Context()
	first := lib.database.Movie.Create().SetCode("AAA-001").SetTitle("A").SaveX(ctx)
	lib.database.File.Create().SetFileID("local-a").SetName("AAA-001.mp4").SetSize(10).
		SetAccountID(domain.LocalAccountID).SetRootID(root).SetPath("AAA-001.mp4").SetMovieID(first.ID).SaveX(ctx)
	second := lib.database.Movie.Create().SetCode("BBB-002").SetTitle("B").SaveX(ctx)
	lib.database.File.Create().SetFileID("local-b").SetName("BBB-002.mp4").SetSize(11).
		SetAccountID(domain.LocalAccountID).SetRootID(root).SetPath("BBB-002.mp4").SetMovieID(second.ID).SaveX(ctx)
	remote := lib.database.Movie.Create().SetCode("CCC-003").SaveX(ctx)
	lib.database.File.Create().SetFileID("remote-c").SetName("CCC-003.mp4").SetSize(12).
		SetAccountID("100").SetRootID("10").SetPath("CCC-003.mp4").SetMovieID(remote.ID).SaveX(ctx)

	page, err := lib.Playables(ctx, "local", "", "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.HasMore {
		t.Fatalf("local page=%+v", page)
	}
	for _, item := range page.Items {
		if item.Kind != "local" || len(item.Videos) != 1 || item.Videos[0].FileID == "remote-c" {
			t.Fatalf("unexpected local item: %+v", item)
		}
	}

	limited, err := lib.Playables(ctx, "local", "", "", 1, 1)
	if err != nil || len(limited.Items) != 1 || !limited.HasMore {
		t.Fatalf("limit=1 page=%+v err=%v", limited, err)
	}
	next, err := lib.Playables(ctx, "local", "", "", 2, 1)
	if err != nil || len(next.Items) != 1 || next.HasMore {
		t.Fatalf("page 2=%+v err=%v", next, err)
	}
}

func TestPlayablesFavoriteIsEmptyAndRemoteNeedsMount(t *testing.T) {
	lib, _ := localPlayFixture(t)
	ctx := t.Context()
	lib.database.Movie.Create().SetCode("AAA-001").SaveX(ctx)

	favorite, err := lib.Playables(ctx, "favorite", "", "", 1, 20)
	if err != nil || len(favorite.Items) != 0 {
		t.Fatalf("favorite=%+v err=%v", favorite, err)
	}
	remote, err := lib.Playables(ctx, "remote", "", "", 1, 20)
	if err != nil || len(remote.Items) != 0 {
		t.Fatalf("remote without mount=%+v err=%v", remote, err)
	}
}

func TestPlayablesSearchesByCodeAndTitle(t *testing.T) {
	lib, root := localPlayFixture(t)
	ctx := t.Context()
	alpha := lib.database.Movie.Create().SetCode("AAA-001").SetTitle("Alpha").SaveX(ctx)
	lib.database.File.Create().SetFileID("f-a").SetName("AAA-001.mp4").SetSize(1).
		SetAccountID(domain.LocalAccountID).SetRootID(root).SetPath("AAA-001.mp4").SetMovieID(alpha.ID).SaveX(ctx)
	beta := lib.database.Movie.Create().SetCode("BBB-002").SetTitle("Beta 特輯").SaveX(ctx)
	lib.database.File.Create().SetFileID("f-b").SetName("BBB-002.mp4").SetSize(1).
		SetAccountID(domain.LocalAccountID).SetRootID(root).SetPath("BBB-002.mp4").SetMovieID(beta.ID).SaveX(ctx)

	byCode, err := lib.Playables(ctx, "local", "", "aaa", 1, 20)
	if err != nil || len(byCode.Items) != 1 || byCode.Items[0].MovieID != alpha.ID {
		t.Fatalf("search by code: %+v %v", byCode, err)
	}
	byTitle, err := lib.Playables(ctx, "local", "", "特輯", 1, 20)
	if err != nil || len(byTitle.Items) != 1 || byTitle.Items[0].MovieID != beta.ID {
		t.Fatalf("search by title: %+v %v", byTitle, err)
	}
	if none, err := lib.Playables(ctx, "local", "", "zzz", 1, 20); err != nil || len(none.Items) != 0 {
		t.Fatalf("no match: %+v %v", none, err)
	}
}
