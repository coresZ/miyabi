package scrape

import (
	"bytes"
	"context"
	"fmt"
	stdimage "image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/tasks"
)

type localMetadata struct{ body []byte }

func (m localMetadata) Resolve(context.Context, domain.MovieRef) (domain.MovieMetadata, error) {
	return domain.MovieMetadata{
		Detail: domain.MovieDetail{Movie: domain.Movie{
			Code: "IPX-123", Title: "片名", ReleaseDate: "2024-01-01", Duration: 120, Rating: 4.5,
			Sources: []domain.SourceID{{Provider: "javdb", ID: "abc"}},
		}},
		Images: []domain.ImageCandidate{{Provider: "javdb", Role: "cover", URL: "https://example.com/c.jpg"}},
	}, nil
}

func (m localMetadata) Fallback(ctx context.Context, ref domain.MovieRef) (domain.MovieMetadata, error) {
	return m.Resolve(ctx, ref)
}

func (m localMetadata) Image(context.Context, domain.ImageCandidate) (domain.Media, error) {
	return domain.Media{Body: m.body, ContentType: "image/png"}, nil
}

type recordingNotifier struct{ changed int }

func (n *recordingNotifier) NotifyLibraryChanged() { n.changed++ }
func (n *recordingNotifier) WakePool()             {}

func posterPNG(t *testing.T) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 600, 900))
	for y := range 900 {
		for x := range 600 {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 120, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestLocalScrapeStoresMetadataWithoutWritingMediaFiles(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	images, err := mediaimage.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mediaRoot := t.TempDir()
	source := domain.LibrarySource{AccountID: domain.LocalAccountID,
		Directory: domain.LibraryDirectory{ID: "src-1", Name: "本地", Path: mediaRoot}}
	notifier := &recordingNotifier{}
	service := New(store.Client, nil, localMetadata{body: posterPNG(t)}, images, notifier, Dependencies{})

	record := store.Client.Movie.Create().SetCode("IPX-123").SaveX(ctx)
	store.Client.File.Create().SetFileID("local-1").SetName("IPX-123.mp4").SetSize(1 << 30).
		SetAccountID(domain.LocalAccountID).SetRootID(domain.LocalAccountID).SetPath("IPX-123.mp4").SetMovieID(record.ID).SaveX(ctx)
	payload, err := tasks.EncodePayload(Payload{MetadataPayload: MetadataPayload{
		Source: source, MovieID: record.ID, Code: "IPX-123",
	}})
	if err != nil {
		t.Fatal(err)
	}
	job := store.Client.Task.Create().SetType(tasks.KindScrape.String()).
		SetResourceKey(fmt.Sprintf("movie:%d", record.ID)).SetPayload(payload).SaveX(ctx)

	if err := service.Scrape(ctx, tasks.Job{ID: job.ID, Type: tasks.KindScrape, Payload: job.Payload}); err != nil {
		t.Fatal(err)
	}

	saved := store.Client.Movie.GetX(ctx, record.ID)
	if saved.ScrapeStatus != movie.ScrapeStatusDone || saved.Metadata == nil || saved.Title != "片名" {
		t.Fatalf("scrape did not store metadata: status=%q title=%q metadata=%v", saved.ScrapeStatus, saved.Title, saved.Metadata)
	}
	if saved.Cover == nil || saved.Poster == nil || len(saved.Fanarts) != 1 {
		t.Fatalf("artwork not stored: cover=%v poster=%v fanarts=%v", saved.Cover, saved.Poster, saved.Fanarts)
	}
	entries, err := os.ReadDir(mediaRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("local scrape wrote %d entries into the media directory", len(entries))
	}
	if notifier.changed == 0 {
		t.Fatal("library change was not announced")
	}
}
