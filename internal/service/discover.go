package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/javdb"
	"github.com/ppxb/miyabi/internal/monitor"
	"github.com/ppxb/miyabi/internal/netx"
)

const (
	javdbDeviceSetting = "javdb.device_uuid"
	javdbRouteSetting  = "javdb.route"
)

type MovieState string
type ReleaseStatus string

const (
	MovieNotInLibrary MovieState = "not_in_library"
	MovieSaving       MovieState = "saving"
	MovieProcessing   MovieState = "processing"
	MovieInLibrary    MovieState = "in_library"
)

const (
	ReleaseUnknown  ReleaseStatus = "unknown"
	ReleaseReleased ReleaseStatus = "released"
	ReleaseUpcoming ReleaseStatus = "upcoming"
)

type DiscoverMovie struct {
	domain.Movie
	LibraryID     int           `json:"library_id,omitempty"`
	State         MovieState    `json:"state"`
	ReleaseStatus ReleaseStatus `json:"release_status"`
}

type DiscoverMovieDetail struct {
	DiscoverMovie
	Zone          domain.Zone             `json:"zone"`
	ActorMovies   []domain.MovieReference `json:"actor_movies"`
	RelatedMovies []domain.MovieReference `json:"related_movies"`
}

type DiscoverMagnet struct {
	domain.Magnet
	URI string `json:"uri"`
}

type JavDBRouteStatus struct {
	Host       string                `json:"host"`
	LatencyMS  int64                 `json:"latency_ms"`
	Active     bool                  `json:"active"`
	Manual     bool                  `json:"manual"`
	Candidates []JavDBRouteCandidate `json:"candidates"`
}

type JavDBRouteCandidate struct {
	Host      string                  `json:"host"`
	LatencyMS int64                   `json:"latency_ms"`
	Status    javdb.RouteAvailability `json:"status"`
}

type persistedRoute struct {
	Host      string `json:"host"`
	LatencyMS int64  `json:"latency_ms"`
	Manual    bool   `json:"manual"`
}

// catalogueClient is the JavDB surface DiscoverService depends on. Tests
// substitute fixtures; production always uses *javdb.Client.
type catalogueClient interface {
	Close()
	Search(context.Context, string, domain.SearchOptions) ([]domain.Movie, error)
	Browse(context.Context, domain.BrowseOptions) ([]domain.Movie, error)
	MovieDetail(context.Context, string) (domain.MovieDetail, error)
	Magnets(context.Context, string) ([]domain.Magnet, error)
	FetchMedia(context.Context, string) (javdb.Media, error)
	Tags(context.Context, domain.Zone) ([]domain.TagCategory, error)
	ResolveMovieID(context.Context, string) (string, error)
	Route() (javdb.RouteStatus, bool)
	SelectRoute(context.Context, string) (javdb.RouteStatus, error)
	Reselect(context.Context) (javdb.RouteStatus, error)
}

// DiscoverService combines JavDB catalogue data with Miyabi's local state.
type DiscoverService struct {
	database *ent.Client
	local    SourceProvider
	javdb    catalogueClient
	lists    *responseCache[[]domain.Movie]
	details  *responseCache[domain.MovieDetail]
	tags     *responseCache[[]domain.TagCategory]
	magnets  *responseCache[[]domain.Magnet]

	routeMu sync.RWMutex
	route   JavDBRouteStatus
}

// NewDiscoverService creates the lazy JavDB client and persists a stable
// anonymous device UUID. It does not perform a network request.
// SourceProvider exposes the mounted library source so catalogue projections
// can tell which local files belong to the current library. B7 turns this
// into catalogue.LocalState implemented by library.
type SourceProvider interface {
	Source() *domain.LibrarySource
}

func NewDiscoverService(
	ctx context.Context,
	database *ent.Client,
	options javdb.Options,
	proxy *netx.ProxyManager,
	local SourceProvider,
) (*DiscoverService, error) {
	deviceUUID, found, err := loadSetting[string](ctx, database, javdbDeviceSetting)
	if err != nil {
		return nil, err
	}
	if !found {
		deviceUUID = options.DeviceUUID
		if deviceUUID == "" {
			deviceUUID, err = javdb.NewDeviceUUID()
			if err != nil {
				return nil, err
			}
		}
		if err := saveSetting(ctx, database, javdbDeviceSetting, deviceUUID); err != nil {
			return nil, err
		}
	}

	route, found, err := loadSetting[persistedRoute](ctx, database, javdbRouteSetting)
	if err != nil {
		return nil, err
	}
	if found {
		options.CachedHost = route.Host
		options.CachedLatency = time.Duration(route.LatencyMS) * time.Millisecond
		options.ManualRoute = route.Manual
	}
	options.DeviceUUID = deviceUUID
	options.Proxy = proxy
	client, err := javdb.New(options)
	if err != nil {
		return nil, err
	}

	return &DiscoverService{
		database: database,
		javdb:    client,
		lists:    newResponseCache[[]domain.Movie](128, time.Minute),
		details:  newResponseCache[domain.MovieDetail](256, 5*time.Minute),
		tags:     newResponseCache[[]domain.TagCategory](5, 24*time.Hour),
		magnets:  newResponseCache[[]domain.Magnet](64, time.Minute),
		local:    local,
		route: JavDBRouteStatus{
			Host:      route.Host,
			LatencyMS: route.LatencyMS,
			Active:    route.Host != "",
			Manual:    route.Manual,
		},
	}, nil
}

func (service *DiscoverService) Close() {
	service.javdb.Close()
}

func (service *DiscoverService) Search(
	ctx context.Context,
	keyword string,
	options domain.SearchOptions,
) ([]DiscoverMovie, error) {
	keyword = strings.TrimSpace(keyword)
	key := fmt.Sprintf("search:%q:%#v", keyword, options)
	movies, err := cachedJavDB(ctx, service, service.lists, key, func(ctx context.Context) ([]domain.Movie, error) {
		return service.javdb.Search(ctx, keyword, options)
	})
	if err != nil {
		return nil, fmt.Errorf("search JavDB: %w", err)
	}
	return service.projectMovies(ctx, movies)
}

func (service *DiscoverService) Browse(
	ctx context.Context,
	options domain.BrowseOptions,
) ([]DiscoverMovie, error) {
	key := fmt.Sprintf("browse:%#v", options)
	movies, err := cachedJavDB(ctx, service, service.lists, key, func(ctx context.Context) ([]domain.Movie, error) {
		return service.javdb.Browse(ctx, options)
	})
	if err != nil {
		return nil, fmt.Errorf("browse JavDB: %w", err)
	}
	return service.projectMovies(ctx, movies)
}

func (service *DiscoverService) CatalogueDetail(ctx context.Context, movieID string) (domain.MovieDetail, error) {
	return cachedJavDB(ctx, service, service.details, movieID, func(ctx context.Context) (domain.MovieDetail, error) {
		detail, err := service.javdb.MovieDetail(ctx, movieID)
		if err != nil {
			return domain.MovieDetail{}, err
		}
		if err := service.completeMovieTags(ctx, &detail); err != nil {
			return domain.MovieDetail{}, err
		}
		return detail, nil
	})
}

func (service *DiscoverService) MovieDetail(ctx context.Context, movieID string) (DiscoverMovieDetail, error) {
	movie, err := service.CatalogueDetail(ctx, movieID)
	if err != nil {
		return DiscoverMovieDetail{}, fmt.Errorf("get JavDB movie detail: %w", err)
	}
	projected, err := service.projectMovies(ctx, []domain.Movie{movie.Movie})
	if err != nil {
		return DiscoverMovieDetail{}, err
	}
	return DiscoverMovieDetail{
		DiscoverMovie: projected[0], Zone: movie.Zone,
		ActorMovies: movie.ActorMovies, RelatedMovies: movie.RelatedMovies,
	}, nil
}

func (service *DiscoverService) Magnets(ctx context.Context, movieID string) ([]DiscoverMagnet, error) {
	magnets, err := cachedJavDB(ctx, service, service.magnets, movieID, func(ctx context.Context) ([]domain.Magnet, error) {
		return service.javdb.Magnets(ctx, movieID)
	})
	if err != nil {
		return nil, fmt.Errorf("get JavDB magnets: %w", err)
	}
	return projectMagnets(magnets), nil
}

func (service *DiscoverService) HasMagnet(ctx context.Context, movieID, hash string) (bool, error) {
	magnets, err := service.Magnets(ctx, movieID)
	if err != nil {
		return false, err
	}
	hash = strings.ToLower(hash)
	for _, m := range magnets {
		if strings.EqualFold(m.Hash, hash) {
			return true, nil
		}
	}
	return false, nil
}

func (service *DiscoverService) MovieCode(ctx context.Context, movieID string) (string, error) {
	movie, err := service.CatalogueDetail(ctx, movieID)
	if err != nil {
		return "", err
	}
	return movie.Code, nil
}

func (service *DiscoverService) MovieSummary(ctx context.Context, movieID string) (monitor.MovieSummary, error) {
	detail, err := service.MovieDetail(ctx, movieID)
	if err != nil {
		return monitor.MovieSummary{}, err
	}
	return monitor.MovieSummary{
		ID:          detail.ID,
		Code:        detail.Code,
		Title:       detail.Title,
		Cover:       detail.Cover,
		ReleaseDate: detail.ReleaseDate,
	}, nil
}

func (service *DiscoverService) FirstMagnetHash(ctx context.Context, movieID string) (string, error) {
	magnets, err := service.Magnets(ctx, movieID)
	if err != nil {
		return "", err
	}
	if len(magnets) == 0 {
		return "", nil
	}
	return strings.ToLower(magnets[0].Hash), nil
}

func projectMagnets(source []domain.Magnet) []DiscoverMagnet {
	result := make([]DiscoverMagnet, len(source))
	for index, item := range source {
		result[index] = DiscoverMagnet{Magnet: item, URI: "magnet:?xt=urn:btih:" + item.Hash}
	}
	return result
}

func (service *DiscoverService) Media(ctx context.Context, rawURL string) (javdb.Media, error) {
	media, err := service.javdb.FetchMedia(ctx, rawURL)
	if err != nil {
		return javdb.Media{}, fmt.Errorf("fetch JavDB media: %w", err)
	}
	return media, nil
}

func (service *DiscoverService) Tags(ctx context.Context, zone domain.Zone) ([]domain.TagCategory, error) {
	categories, err := cachedJavDB(ctx, service, service.tags, string(zone), func(ctx context.Context) ([]domain.TagCategory, error) {
		return service.javdb.Tags(ctx, zone)
	})
	if err != nil {
		return nil, fmt.Errorf("get JavDB tags: %w", err)
	}
	return categories, nil
}

func (service *DiscoverService) ResolveMovieID(ctx context.Context, code string) (string, error) {
	id, err := service.javdb.ResolveMovieID(ctx, code)
	if err != nil {
		return "", fmt.Errorf("resolve JavDB movie ID: %w", err)
	}
	if err := service.persistActiveRoute(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (service *DiscoverService) Route() JavDBRouteStatus {
	status, active := service.javdb.Route()
	result := JavDBRouteStatus{
		Host: status.Host, LatencyMS: status.Latency.Milliseconds(),
		Active: active, Manual: status.Manual,
		Candidates: make([]JavDBRouteCandidate, len(status.Candidates)),
	}
	for index, candidate := range status.Candidates {
		result.Candidates[index] = JavDBRouteCandidate{
			Host: candidate.Host, LatencyMS: candidate.Latency.Milliseconds(), Status: candidate.Status,
		}
	}
	if !active {
		service.routeMu.RLock()
		result.Host = service.route.Host
		result.LatencyMS = service.route.LatencyMS
		result.Manual = service.route.Manual
		service.routeMu.RUnlock()
	}
	return result
}

func (service *DiscoverService) SelectRoute(ctx context.Context, host string) (JavDBRouteStatus, error) {
	if host == "" {
		return service.Reselect(ctx)
	}
	if _, err := service.javdb.SelectRoute(ctx, host); err != nil {
		return JavDBRouteStatus{}, fmt.Errorf("select JavDB route: %w", err)
	}
	if err := service.persistActiveRoute(ctx); err != nil {
		return JavDBRouteStatus{}, err
	}
	return service.Route(), nil
}

func (service *DiscoverService) Reselect(ctx context.Context) (JavDBRouteStatus, error) {
	if _, err := service.javdb.Reselect(ctx); err != nil {
		return JavDBRouteStatus{}, fmt.Errorf("reselect JavDB route: %w", err)
	}
	if err := service.persistActiveRoute(ctx); err != nil {
		return JavDBRouteStatus{}, err
	}
	return service.Route(), nil
}

func (service *DiscoverService) projectMovies(
	ctx context.Context,
	source []domain.Movie,
) ([]DiscoverMovie, error) {
	if len(source) == 0 {
		return []DiscoverMovie{}, nil
	}
	identities := make([]MovieIdentity, len(source))
	for index, item := range source {
		identities[index] = MovieIdentity{ID: item.ID, Code: item.Code}
	}
	states, err := service.MovieStates(ctx, identities)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	result := make([]DiscoverMovie, len(source))
	for index, item := range source {
		releaseStatus := ReleaseUnknown
		item.ReleaseDate = strings.TrimSpace(item.ReleaseDate)
		if item.ReleaseDate != "" {
			releaseDate, err := time.ParseInLocation("2006-01-02", item.ReleaseDate, time.Local)
			if err != nil {
				slog.WarnContext(ctx, "invalid JavDB release date; omitting date",
					"movie_id", item.ID, "field", "release_date", "value", item.ReleaseDate)
				item.ReleaseDate = ""
			} else if releaseDate.After(today) {
				releaseStatus = ReleaseUpcoming
			} else {
				releaseStatus = ReleaseReleased
			}
		}
		result[index] = DiscoverMovie{
			Movie:         item,
			LibraryID:     states[index].LibraryID,
			State:         states[index].State,
			ReleaseStatus: releaseStatus,
		}
	}
	return result, nil
}

func (service *DiscoverService) persistActiveRoute(ctx context.Context) error {
	service.routeMu.Lock()
	defer service.routeMu.Unlock()
	active, ok := service.javdb.Route()
	if !ok {
		return nil
	}
	route := persistedRoute{Host: active.Host, LatencyMS: active.Latency.Milliseconds(), Manual: active.Manual}
	unchanged := service.route.Active && service.route.Host == route.Host &&
		service.route.LatencyMS == route.LatencyMS && service.route.Manual == route.Manual
	if unchanged {
		return nil
	}
	if err := saveSetting(ctx, service.database, javdbRouteSetting, route); err != nil {
		return fmt.Errorf("cache JavDB route: %w", err)
	}
	service.route = JavDBRouteStatus{
		Host:      route.Host,
		LatencyMS: route.LatencyMS,
		Active:    true,
		Manual:    route.Manual,
	}
	return nil
}
