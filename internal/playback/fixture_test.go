package playback

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/library/scan"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

type panStub struct {
	drive.Client
	account        func(context.Context, string) (pan.Account, error)
	beginLogin     func(context.Context) (*pan.Login, error)
	loginStatus    func(context.Context, *pan.Login) (pan.LoginState, error)
	exchangeToken  func(context.Context, *pan.Login) (pan.Tokens, error)
	refreshToken   func(context.Context, string) (pan.Tokens, error)
	list           func(context.Context, string, string, int, int) (pan.FilePage, error)
	info           func(context.Context, string, string) (pan.FileInfo, error)
	readMetadata   func(context.Context, string, string, int64) ([]byte, error)
	uploadMetadata func(context.Context, string, string, string, []byte) error
	addOffline     func(context.Context, string, string, string) (string, error)
	removeOffline  func(context.Context, string, string) error
	offlineTasks   func(context.Context, string, int) (pan.OfflinePage, error)
	playURL        func(context.Context, string, string) ([]pan.PlaySource, error)
	openMedia      func(context.Context, string, string, http.Header) (*http.Response, error)
}

func (client *panStub) Close() {
	if client.Client != nil {
		client.Client.Close()
	}
}

func (client *panStub) Account(ctx context.Context, token string) (pan.Account, error) {
	if client.account != nil {
		return client.account(ctx, token)
	}
	if client.Client != nil {
		return client.Client.Account(ctx, token)
	}
	return pan.Account{}, nil
}

func (client *panStub) BeginLogin(ctx context.Context) (*pan.Login, error) {
	if client.beginLogin != nil {
		return client.beginLogin(ctx)
	}
	if client.Client != nil {
		return client.Client.BeginLogin(ctx)
	}
	return &pan.Login{QRCode: []byte("fixture")}, nil
}

func (client *panStub) LoginStatus(ctx context.Context, login *pan.Login) (pan.LoginState, error) {
	if client.loginStatus != nil {
		return client.loginStatus(ctx, login)
	}
	if client.Client != nil {
		return client.Client.LoginStatus(ctx, login)
	}
	return pan.LoginAuthorized, nil
}

func (client *panStub) ExchangeToken(ctx context.Context, login *pan.Login) (pan.Tokens, error) {
	if client.exchangeToken != nil {
		return client.exchangeToken(ctx, login)
	}
	if client.Client != nil {
		return client.Client.ExchangeToken(ctx, login)
	}
	return panTestTokens("fixture"), nil
}

func (client *panStub) RefreshToken(ctx context.Context, refresh string) (pan.Tokens, error) {
	if client.refreshToken != nil {
		return client.refreshToken(ctx, refresh)
	}
	if client.Client != nil {
		return client.Client.RefreshToken(ctx, refresh)
	}
	return panTestTokens("refreshed"), nil
}

func (client *panStub) List(ctx context.Context, token, id string, offset, limit int) (pan.FilePage, error) {
	if client.list != nil {
		return client.list(ctx, token, id, offset, limit)
	}
	if client.Client != nil {
		return client.Client.List(ctx, token, id, offset, limit)
	}
	return pan.FilePage{}, nil
}

func (client *panStub) Info(ctx context.Context, token, id string) (pan.FileInfo, error) {
	if client.info != nil {
		return client.info(ctx, token, id)
	}
	if client.Client != nil {
		return client.Client.Info(ctx, token, id)
	}
	return pan.FileInfo{}, nil
}

func (client *panStub) ReadMetadata(ctx context.Context, token, pickCode string, size int64) ([]byte, error) {
	if client.readMetadata != nil {
		return client.readMetadata(ctx, token, pickCode, size)
	}
	if client.Client != nil {
		return client.Client.ReadMetadata(ctx, token, pickCode, size)
	}
	return nil, nil
}

func (client *panStub) UploadMetadata(ctx context.Context, token, parentID, name string, content []byte) error {
	if client.uploadMetadata != nil {
		return client.uploadMetadata(ctx, token, parentID, name, content)
	}
	if client.Client != nil {
		return client.Client.UploadMetadata(ctx, token, parentID, name, content)
	}
	return nil
}

func (client *panStub) AddOffline(ctx context.Context, token, url, saveDirID string) (string, error) {
	if client.addOffline != nil {
		return client.addOffline(ctx, token, url, saveDirID)
	}
	if client.Client != nil {
		return client.Client.AddOffline(ctx, token, url, saveDirID)
	}
	return "", nil
}

func (client *panStub) RemoveOffline(ctx context.Context, token, hash string) error {
	if client.removeOffline != nil {
		return client.removeOffline(ctx, token, hash)
	}
	if client.Client != nil {
		return client.Client.RemoveOffline(ctx, token, hash)
	}
	return nil
}

func (client *panStub) OfflineTasks(ctx context.Context, token string, page int) (pan.OfflinePage, error) {
	if client.offlineTasks != nil {
		return client.offlineTasks(ctx, token, page)
	}
	if client.Client != nil {
		return client.Client.OfflineTasks(ctx, token, page)
	}
	return pan.OfflinePage{}, nil
}

func (client *panStub) PlayURL(ctx context.Context, token, pickCode string) ([]pan.PlaySource, error) {
	if client.playURL != nil {
		return client.playURL(ctx, token, pickCode)
	}
	if client.Client != nil {
		return client.Client.PlayURL(ctx, token, pickCode)
	}
	return nil, nil
}

func (client *panStub) OpenMedia(ctx context.Context, method, address string, headers http.Header) (*http.Response, error) {
	if client.openMedia != nil {
		return client.openMedia(ctx, method, address, headers)
	}
	if client.Client != nil {
		return client.Client.OpenMedia(ctx, method, address, headers)
	}
	return pan.New().OpenMedia(ctx, method, address, headers)
}

func panTestTokens(prefix string) pan.Tokens {
	return pan.Tokens{AccessToken: prefix + "-access", RefreshToken: prefix + "-refresh", ExpiresAt: time.Now().Add(time.Hour)}
}

func directoryPage(directory domain.LibraryDirectory) pan.FilePage {
	page := pan.FilePage{Path: []pan.Directory{{ID: "0", Name: "Root"}}}
	segments := strings.Split(strings.Trim(directory.Path, "/"), "/")
	for i, name := range segments[:len(segments)-1] {
		page.Path = append(page.Path, pan.Directory{ID: fmt.Sprintf("ancestor-%d", i), Name: name})
	}
	name := directory.Name
	if name == "" {
		name = segments[len(segments)-1]
	}
	page.Path = append(page.Path, pan.Directory{ID: directory.ID, Name: name})
	return page
}

func loginAccount(t testing.TB, d *drive.Drive, client *panStub, accountID string) {
	t.Helper()
	client.account = func(context.Context, string) (pan.Account, error) { return pan.Account{ID: accountID}, nil }
	login, err := d.BeginLogin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status, err := d.LoginStatus(t.Context(), login.ID); err != nil || status.State != pan.LoginAuthorized {
		t.Fatalf("fixture login = %+v, %v", status, err)
	}
}

func mountSource(t testing.TB, d *drive.Drive, client *panStub, source domain.LibrarySource) {
	t.Helper()
	status, err := d.Account(t.Context())
	if err != nil || !status.Connected || status.Account == nil || status.Account.ID != source.AccountID {
		loginAccount(t, d, client, source.AccountID)
	}
	if source.Directory.ID == "" {
		return
	}
	previous := client.list
	client.list = func(ctx context.Context, token, id string, offset, limit int) (pan.FilePage, error) {
		if id == source.Directory.ID {
			return directoryPage(source.Directory), nil
		}
		if previous != nil {
			return previous(ctx, token, id, offset, limit)
		}
		return pan.FilePage{}, nil
	}
	defer func() { client.list = previous }()
	if _, err := d.SelectDirectory(t.Context(), source.Directory.ID); err != nil {
		t.Fatal(err)
	}
}

var fixtureStubs sync.Map

func stubOf(t testing.TB, d *drive.Drive) *panStub {
	t.Helper()
	client, ok := fixtureStubs.Load(d)
	if !ok {
		t.Fatal("drive was not created by newMountedDrive")
	}
	return client.(*panStub)
}

func newMountedDrive(t testing.TB, database *ent.Client, client *panStub, source domain.LibrarySource) *drive.Drive {
	t.Helper()
	d, err := drive.NewWithClient(t.Context(), database, client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	fixtureStubs.Store(d, client)
	mountSource(t, d, client, source)
	return d
}

func authorizationVersion(t testing.TB, d *drive.Drive) uint64 {
	t.Helper()
	sess, err := d.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return sess.Version()
}

func playFixture(t *testing.T) (*Service, domain.LibrarySource) {
	t.Helper()
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	source := domain.LibrarySource{AccountID: "100", Directory: domain.LibraryDirectory{ID: "10", Name: "Movies", Path: "/Movies"}}
	driveSvc := newMountedDrive(t, store.Client, &panStub{}, source)
	taskSvc := tasks.NewService(store.Client, tasks.NewRegistry())
	queued, err := taskSvc.EnqueueScan(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	payload := scan.Payload{Source: source, Scan: domain.ScanProgress{Stage: "scanning"}}
	if err := scan.ProcessScanPage(t.Context(), store.Client, queued.ID, "fixture", "/Movies", []scan.Video{
		scan.IdentifyVideo(pan.File{ID: "101", ParentID: "10", Name: "ABP-001-CD1.mp4", Size: 1 << 30}),
		scan.IdentifyVideo(pan.File{ID: "102", ParentID: "10", Name: "ABP-001-CD2.mkv", Size: 1 << 30}),
	}, &payload, nil, taskSvc); err != nil {
		t.Fatal(err)
	}
	service := New(store.Client, driveSvc)
	t.Cleanup(service.Close)
	return service, payload.Source
}

func panTestGate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	return gate, release
}

func awaitPan[T any](t *testing.T, ready <-chan T) T {
	t.Helper()
	select {
	case value := <-ready:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for concurrent 115 operation")
		var zero T
		return zero
	}
}
