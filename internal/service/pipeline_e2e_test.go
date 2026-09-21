package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/ent/task"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/javdb"
	"github.com/ppxb/miyabi/internal/library"
	scrapePkg "github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

// fakeDrive is an in-memory 115 account: a directory tree, file contents keyed
// by pick code, and a record of every sidecar upload in call order.
type fakeDrive struct {
	mu        sync.Mutex
	accountID string
	dirs      map[string]fakeDirectory
	files     map[string]pan.File
	contents  map[string][]byte
	uploads   []fakeUpload
	nextID    int
}

type fakeDirectory struct {
	name, parentID string
}

type fakeUpload struct {
	directory, name string
	body            []byte
}

func newFakeDrive(accountID string) *fakeDrive {
	return &fakeDrive{
		accountID: accountID,
		dirs:      map[string]fakeDirectory{"0": {name: ""}},
		files:     make(map[string]pan.File),
		contents:  make(map[string][]byte),
		nextID:    1000,
	}
}

func (drive *fakeDrive) addDirectory(id, parentID, name string) {
	drive.mu.Lock()
	defer drive.mu.Unlock()
	drive.dirs[id] = fakeDirectory{name: name, parentID: parentID}
}

func (drive *fakeDrive) addFile(id, parentID, name string, size int64, body []byte) pan.File {
	drive.mu.Lock()
	defer drive.mu.Unlock()
	entry := pan.File{ID: id, ParentID: parentID, Name: name, Size: size, PickCode: "pc-" + id, SHA1: pan.SHA1(body)}
	drive.files[id] = entry
	drive.contents[entry.PickCode] = body
	return entry
}

func (drive *fakeDrive) ancestors(directoryID string) []pan.Directory {
	var chain []pan.Directory
	for id := directoryID; ; {
		directory, ok := drive.dirs[id]
		if !ok {
			return nil
		}
		chain = append(chain, pan.Directory{ID: id, Name: directory.name})
		if id == "0" {
			break
		}
		id = directory.parentID
	}
	slices.Reverse(chain)
	return chain
}

func (drive *fakeDrive) Close() {}

func (drive *fakeDrive) Account(context.Context, string) (pan.Account, error) {
	return pan.Account{ID: drive.accountID, Name: "fixture"}, nil
}

func (drive *fakeDrive) List(_ context.Context, _ string, directoryID string, offset, limit int) (pan.FilePage, error) {
	drive.mu.Lock()
	defer drive.mu.Unlock()
	path := drive.ancestors(directoryID)
	if path == nil {
		return pan.FilePage{}, pan.ErrNotFound
	}
	var entries []pan.File
	for id, directory := range drive.dirs {
		if id != "0" && directory.parentID == directoryID {
			entries = append(entries, pan.File{ID: id, ParentID: directoryID, Name: directory.name, IsDirectory: true})
		}
	}
	for _, entry := range drive.files {
		if entry.ParentID == directoryID {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	end := min(offset+limit, len(entries))
	if offset > len(entries) {
		offset = len(entries)
	}
	return pan.FilePage{Files: entries[offset:end], Path: path, Total: len(entries), HasMore: end < len(entries)}, nil
}

func (drive *fakeDrive) Info(_ context.Context, _ string, id string) (pan.FileInfo, error) {
	drive.mu.Lock()
	defer drive.mu.Unlock()
	if entry, ok := drive.files[id]; ok {
		return pan.FileInfo{File: entry, Path: drive.ancestors(entry.ParentID)}, nil
	}
	if directory, ok := drive.dirs[id]; ok {
		return pan.FileInfo{
			File: pan.File{ID: id, ParentID: directory.parentID, Name: directory.name, IsDirectory: true},
			Path: drive.ancestors(directory.parentID),
		}, nil
	}
	return pan.FileInfo{}, pan.ErrNotFound
}

func (drive *fakeDrive) ReadMetadata(_ context.Context, _ string, pickCode string, limit int64) ([]byte, error) {
	drive.mu.Lock()
	defer drive.mu.Unlock()
	body, ok := drive.contents[pickCode]
	if !ok {
		return nil, pan.ErrNotFound
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("fixture sidecar exceeds %d bytes", limit)
	}
	return body, nil
}

func (drive *fakeDrive) UploadMetadata(_ context.Context, _ string, directoryID, name string, body []byte) error {
	drive.mu.Lock()
	defer drive.mu.Unlock()
	if _, ok := drive.dirs[directoryID]; !ok {
		return pan.ErrNotFound
	}
	drive.nextID++
	id := fmt.Sprint(drive.nextID)
	entry := pan.File{ID: id, ParentID: directoryID, Name: name, Size: int64(len(body)), PickCode: "pc-" + id, SHA1: pan.SHA1(body)}
	drive.files[id] = entry
	drive.contents[entry.PickCode] = bytes.Clone(body)
	drive.uploads = append(drive.uploads, fakeUpload{directory: directoryID, name: name, body: bytes.Clone(body)})
	return nil
}

func (drive *fakeDrive) uploadedNames(directoryID string) []string {
	drive.mu.Lock()
	defer drive.mu.Unlock()
	var names []string
	for _, upload := range drive.uploads {
		if upload.directory == directoryID {
			names = append(names, upload.name)
		}
	}
	return names
}

// fakeCatalogue answers JavDB lookups from memory and counts upstream calls.
type fakeCatalogue struct {
	mu      sync.Mutex
	ids     map[string]string
	details map[string]domain.MovieDetail
	cover   []byte
	calls   map[string]int
}

func (catalogue *fakeCatalogue) count(name string) {
	catalogue.mu.Lock()
	defer catalogue.mu.Unlock()
	if catalogue.calls == nil {
		catalogue.calls = make(map[string]int)
	}
	catalogue.calls[name]++
}

func (catalogue *fakeCatalogue) Close() {}

func (catalogue *fakeCatalogue) Search(context.Context, string, domain.SearchOptions) ([]domain.Movie, error) {
	catalogue.count("search")
	return nil, fmt.Errorf("search is not part of this fixture")
}

func (catalogue *fakeCatalogue) Browse(context.Context, domain.BrowseOptions) ([]domain.Movie, error) {
	catalogue.count("browse")
	return nil, fmt.Errorf("browse is not part of this fixture")
}

func (catalogue *fakeCatalogue) MovieDetail(_ context.Context, id string) (domain.MovieDetail, error) {
	catalogue.count("detail")
	detail, ok := catalogue.details[id]
	if !ok {
		return domain.MovieDetail{}, &javdb.APIError{Message: "movie not found"}
	}
	return detail, nil
}

func (catalogue *fakeCatalogue) Magnets(context.Context, string) ([]domain.Magnet, error) {
	catalogue.count("magnets")
	return []domain.Magnet{}, nil
}

func (catalogue *fakeCatalogue) FetchMedia(_ context.Context, rawURL string) (javdb.Media, error) {
	catalogue.count("media")
	if !strings.HasPrefix(rawURL, "https://media.example/") {
		return javdb.Media{}, fmt.Errorf("unexpected media URL %q", rawURL)
	}
	return javdb.Media{ContentType: "image/jpeg", Body: catalogue.cover}, nil
}

func (catalogue *fakeCatalogue) Tags(context.Context, domain.Zone) ([]domain.TagCategory, error) {
	catalogue.count("tags")
	return []domain.TagCategory{}, nil
}

func (catalogue *fakeCatalogue) ResolveMovieID(_ context.Context, code string) (string, error) {
	catalogue.count("resolve")
	id, ok := catalogue.ids[code]
	if !ok {
		return "", fmt.Errorf("catalogue number %s was not found on JavDB", code)
	}
	return id, nil
}

func (catalogue *fakeCatalogue) Route() (javdb.RouteStatus, bool) { return javdb.RouteStatus{}, false }

func (catalogue *fakeCatalogue) SelectRoute(context.Context, string) (javdb.RouteStatus, error) {
	return javdb.RouteStatus{}, fmt.Errorf("route selection is not part of this fixture")
}

func (catalogue *fakeCatalogue) Reselect(context.Context) (javdb.RouteStatus, error) {
	return javdb.RouteStatus{}, fmt.Errorf("route selection is not part of this fixture")
}

func fixtureJPEG(t testing.TB, width, height int) []byte {
	t.Helper()
	var body bytes.Buffer
	if err := jpeg.Encode(&body, image.NewRGBA(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

// pipelineFixture wires every service that participates in the scan → scrape
// → cover workflow against the in-memory 115 account and catalogue.
type pipelineFixture struct {
	store     *database.Store
	drive     *fakeDrive
	catalogue *fakeCatalogue
	tasks     *tasks.Service
	library   *library.Service
	scrape    *scrapePkg.Service
	discover  *DiscoverService
	source    domain.LibrarySource
}

func newPipelineFixture(t *testing.T) *pipelineFixture {
	t.Helper()
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	source := domain.LibrarySource{AccountID: "100", Directory: domain.LibraryDirectory{ID: "10", Name: "Movies", Path: "/Movies"}}
	drive := newFakeDrive(source.AccountID)
	drive.addDirectory("10", "0", "Movies")

	taskSvc := tasks.NewService(store.Client, tasks.NewRegistry())
	d := newMountedDrive(t, store.Client, &panStub{
		account: drive.Account, list: drive.List, info: drive.Info,
		readMetadata: drive.ReadMetadata, uploadMetadata: drive.UploadMetadata,
	}, source)

	images, err := mediaimage.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	discover, err := NewDiscoverService(ctx, store.Client, javdb.Options{}, nil, d)
	if err != nil {
		t.Fatal(err)
	}
	discover.javdb.Close()
	catalogue := &fakeCatalogue{ids: make(map[string]string), details: make(map[string]domain.MovieDetail), cover: fixtureJPEG(t, 600, 400)}
	discover.javdb = catalogue
	library := library.New(store.Client, d, taskSvc, images)
	scrape := scrapePkg.New(store.Client, d, discover, images, taskSvc)
	taskSvc.Registry().Register(tasks.NewHandler(tasks.KindScan, library.Scan, library.Finished))
	taskSvc.Registry().Register(tasks.NewHandler(tasks.KindScrape, scrape.Scrape, scrape.Finished))
	taskSvc.Registry().Register(tasks.NewHandler(tasks.KindCover, scrape.Cover, scrape.Finished))
	return &pipelineFixture{
		store: store, drive: drive, catalogue: catalogue, tasks: taskSvc,
		library: library, discover: discover, scrape: scrape, source: source,
	}
}

func (fixture *pipelineFixture) addCatalogueMovie(detail domain.MovieDetail) {
	fixture.catalogue.ids[detail.Code] = detail.ID
	fixture.catalogue.details[detail.ID] = detail
}

// runQueue mirrors worker.Pool with one worker: claim, handle, finish until
// nothing is queued. It returns the tasks it executed in order.
func (fixture *pipelineFixture) runQueue(t *testing.T) []tasks.Job {
	t.Helper()
	ctx := t.Context()
	handlers := map[tasks.Kind]func(context.Context, tasks.Job) error{
		tasks.KindScan: fixture.library.Scan, tasks.KindScrape: fixture.scrape.Scrape, tasks.KindCover: fixture.scrape.Cover,
	}
	types := []tasks.Kind{tasks.KindCover, tasks.KindScan, tasks.KindScrape}
	var executed []tasks.Job
	for range 20 {
		job, err := fixture.tasks.Queue().Claim(ctx, types)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			return executed
		}
		executed = append(executed, *job)
		if err := fixture.tasks.Queue().Finish(ctx, job.ID, handlers[job.Type](ctx, *job)); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("task queue did not drain within 20 jobs")
	return nil
}

func (fixture *pipelineFixture) tasksOfType(t *testing.T, kind string) []*ent.Task {
	t.Helper()
	records, err := fixture.store.Client.Task.Query().Where(task.TypeEQ(kind)).Order(ent.Asc(task.FieldID)).All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func fixtureDetail() domain.MovieDetail {
	return domain.MovieDetail{
		Movie: domain.Movie{
			ID: "movie-exact", Code: "ABP-123", Title: "Localized title", OriginTitle: "Original title",
			ReleaseDate: "2026-08-01", Duration: 120, Rating: 4.5,
			Thumbnail: "https://media.example/thumb.jpg", Cover: "https://media.example/cover.jpg",
			Actors: []domain.Actor{
				{ID: "actor-1", Name: "Actor", NameZHT: "演員", Gender: "female", Avatar: "https://media.example/actor.jpg"},
				{ID: "actor-2", Name: "Actor Two", Gender: "male", Avatar: "https://media.example/actor-two.jpg"},
			},
			Tags:     []domain.Tag{{ID: "tag-1", Name: "Tag", NameZHT: "標籤", CategoryID: "category-1"}},
			Series:   &domain.Series{ID: "series-1", Name: "Series"},
			Maker:    &domain.Maker{ID: "maker-1", Name: "Maker"},
			Director: &domain.Director{ID: "director-1", Name: "Director"},
		},
		Zone: domain.ZoneCensored,
	}
}

func TestPipelineScansScrapesAndWritesSidecarsEndToEnd(t *testing.T) {
	fixture := newPipelineFixture(t)
	ctx := t.Context()
	fixture.drive.addDirectory("11", "10", "ABP-123")
	video := fixture.drive.addFile("101", "11", "ABP-123.mp4", 2<<30, []byte("video"))
	fixture.addCatalogueMovie(fixtureDetail())

	if _, err := fixture.library.StartScan(ctx); err != nil {
		t.Fatal(err)
	}
	executed := fixture.runQueue(t)
	kinds := make([]string, len(executed))
	for index, job := range executed {
		kinds[index] = string(job.Type)
	}
	if !slices.Equal(kinds, []string{"scan", "scrape", "cover"}) {
		t.Fatalf("executed task chain = %v", kinds)
	}
	for _, kind := range kinds {
		for _, record := range fixture.tasksOfType(t, kind) {
			if record.Status != task.StatusDone {
				t.Fatalf("%s task %d = %s: %v", kind, record.ID, record.Status, valueOrZero(record.Error))
			}
		}
	}

	// The catalogue was consulted exactly once per resource; the rest is cached or local.
	if fixture.catalogue.calls["resolve"] != 1 || fixture.catalogue.calls["detail"] != 1 || fixture.catalogue.calls["media"] != 1 {
		t.Fatalf("catalogue calls = %v", fixture.catalogue.calls)
	}

	record, err := fixture.store.Client.Movie.Query().Where(movie.CodeEQ("ABP-123")).WithActors().WithTags().WithFiles().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if valueOrZero(record.JavdbID) != "movie-exact" || record.Title != "Localized title" || record.ScrapeStatus != movie.ScrapeStatusDone {
		t.Fatalf("movie = %+v", record)
	}
	if valueOrZero(record.Duration) != 120 || valueOrZero(record.Rating) != 4.5 || record.ReleaseDate == nil || record.ReleaseDate.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("movie metadata = duration %v rating %v release %v", record.Duration, record.Rating, record.ReleaseDate)
	}
	if valueOrZero(record.DirectorName) != "Director" || valueOrZero(record.MakerName) != "Maker" || valueOrZero(record.SeriesName) != "Series" {
		t.Fatalf("movie entities = %+v", record)
	}
	if len(record.Edges.Actors) != 2 || len(record.Edges.Tags) != 1 || len(record.Edges.Files) != 1 || record.Edges.Files[0].FileID != video.ID {
		t.Fatalf("movie edges = actors %d tags %d files %+v", len(record.Edges.Actors), len(record.Edges.Tags), record.Edges.Files)
	}
	artwork := scrapePkg.MovieArtwork(record)
	if exists, err := fixture.library.Images().Exists(artwork); err != nil || !exists {
		t.Fatalf("artwork %+v cached = %t, %v", artwork, exists, err)
	}

	// Sidecars land next to the video, images first and the NFO last.
	if names := fixture.drive.uploadedNames("11"); !slices.Equal(names, []string{"poster.jpg", "fanart.jpg", "ABP-123.nfo"}) {
		t.Fatalf("uploaded sidecars = %v", names)
	}
	doc, err := nfo.Decode(fixture.drive.uploads[2].body)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Code != "ABP-123" || doc.JavDBID() != "movie-exact" || doc.Title != "Localized title" || doc.Premiered != "2026-08-01" {
		t.Fatalf("nfo = %+v", doc)
	}
	if doc.Poster() != "poster.jpg" || doc.Fanart != "fanart.jpg" || len(doc.Actors) != 2 || len(doc.Tags) != 1 || doc.Studio.Name != "Maker" {
		t.Fatalf("nfo references = poster %q fanart %q actors %d tags %d studio %+v", doc.Poster(), doc.Fanart, len(doc.Actors), len(doc.Tags), doc.Studio)
	}
	poster, err := fixture.library.Images().ReadURL(artwork.Poster)
	if err != nil || !bytes.Equal(poster, fixture.drive.uploads[0].body) {
		t.Fatalf("uploaded poster differs from cached poster: %v", err)
	}

	// The scan workflow reports the metadata chain as complete.
	infos, err := fixture.tasks.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Status != task.StatusDone || infos[0].Scan.MetadataTotal != 1 || infos[0].Scan.MetadataCompleted != 1 || infos[0].Scan.Movies != 1 {
		t.Fatalf("scan workflow = %+v", infos)
	}
	states, err := fixture.discover.MovieStates(ctx, []MovieIdentity{{ID: "movie-exact", Code: "ABP-123"}})
	if err != nil || len(states) != 1 || states[0].State != MovieInLibrary || states[0].LibraryID != record.ID {
		t.Fatalf("movie states = %+v, %v", states, err)
	}
	page, err := fixture.library.Movies(ctx, 1, 20)
	if err != nil || page.Total != 1 || len(page.Movies) != 1 || page.Movies[0].Fanart != artwork.Fanart || page.Movies[0].ScrapeStatus != movie.ScrapeStatusDone {
		t.Fatalf("library page = %+v, %v", page, err)
	}

	// A rescan finds the sidecars it wrote and schedules no further metadata work.
	if _, err := fixture.library.StartScan(ctx); err != nil {
		t.Fatal(err)
	}
	executed = fixture.runQueue(t)
	if len(executed) != 1 || executed[0].Type != "scan" {
		t.Fatalf("rescan executed %v", executed)
	}
	if scrapes := fixture.tasksOfType(t, "scrape"); len(scrapes) != 1 {
		t.Fatalf("rescan created %d scrape tasks", len(scrapes))
	}
	if fixture.catalogue.calls["detail"] != 1 {
		t.Fatalf("rescan contacted the catalogue again: %v", fixture.catalogue.calls)
	}
}

func TestPipelineReusesUserNFOInsteadOfCatalogue(t *testing.T) {
	fixture := newPipelineFixture(t)
	ctx := t.Context()
	fixture.drive.addDirectory("11", "10", "ABP-123")
	fixture.drive.addFile("101", "11", "ABP-123.mp4", 2<<30, []byte("video"))
	poster, fanart := fixtureJPEG(t, 200, 300), fixtureJPEG(t, 600, 400)
	fixture.drive.addFile("102", "11", "poster.jpg", int64(len(poster)), poster)
	fixture.drive.addFile("103", "11", "fanart.jpg", int64(len(fanart)), fanart)
	doc := nfo.Movie{
		Title: "User title", Code: "ABP-123", Premiered: "2020-01-02", Runtime: 90,
		IDs:    []nfo.UniqueID{{Type: "javdb", Default: true, Value: "movie-user"}},
		Actors: []nfo.Actor{{ID: "actor-9", Name: "Sidecar actor"}},
		Thumbs: []nfo.Thumb{{Aspect: "poster", Path: "poster.jpg"}}, Fanart: "fanart.jpg",
	}
	body, err := nfo.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	fixture.drive.addFile("104", "11", "ABP-123.nfo", int64(len(body)), body)

	if _, err := fixture.library.StartScan(ctx); err != nil {
		t.Fatal(err)
	}
	fixture.runQueue(t)
	for _, kind := range []string{"scan", "scrape", "cover"} {
		for _, record := range fixture.tasksOfType(t, kind) {
			if record.Status != task.StatusDone {
				t.Fatalf("%s task %d = %s: %v", kind, record.ID, record.Status, valueOrZero(record.Error))
			}
		}
	}
	if len(fixture.catalogue.calls) != 0 {
		t.Fatalf("user NFO must not trigger catalogue calls: %v", fixture.catalogue.calls)
	}
	record, err := fixture.store.Client.Movie.Query().Where(movie.CodeEQ("ABP-123")).WithActors().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Title != "User title" || valueOrZero(record.JavdbID) != "movie-user" || len(record.Edges.Actors) != 1 || record.ScrapeStatus != movie.ScrapeStatusDone {
		t.Fatalf("movie from NFO = %+v actors %d", record, len(record.Edges.Actors))
	}
	if uploads := fixture.drive.uploadedNames("11"); len(uploads) != 0 {
		t.Fatalf("existing sidecars were rewritten: %v", uploads)
	}
}

func TestPipelineMarksMovieFailedWhenCatalogueLacksIt(t *testing.T) {
	fixture := newPipelineFixture(t)
	ctx := t.Context()
	fixture.drive.addDirectory("11", "10", "ZZZ-999")
	fixture.drive.addFile("101", "11", "ZZZ-999.mp4", 2<<30, []byte("video"))

	if _, err := fixture.library.StartScan(ctx); err != nil {
		t.Fatal(err)
	}
	executed := fixture.runQueue(t)
	if len(executed) != 2 || executed[1].Type != tasks.KindScrape {
		t.Fatalf("executed %v", executed)
	}
	scrapes := fixture.tasksOfType(t, "scrape")
	if len(scrapes) != 1 || scrapes[0].Status != task.StatusFailed || !strings.Contains(valueOrZero(scrapes[0].Error), "ZZZ-999") {
		t.Fatalf("scrape task = %+v", scrapes)
	}
	record, err := fixture.store.Client.Movie.Query().Where(movie.CodeEQ("ZZZ-999")).Only(ctx)
	if err != nil || record.ScrapeStatus != movie.ScrapeStatusFailed {
		t.Fatalf("movie = %+v, %v", record, err)
	}
	if covers := fixture.tasksOfType(t, "cover"); len(covers) != 0 {
		t.Fatalf("failed scrape queued artwork: %v", covers)
	}
	infos, err := fixture.tasks.List(ctx)
	if err != nil || len(infos) != 1 || infos[0].Status != task.StatusFailed || infos[0].Error == nil {
		t.Fatalf("scan workflow = %+v, %v", infos, err)
	}
}
