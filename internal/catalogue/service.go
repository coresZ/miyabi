package catalogue

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/setting"
	"github.com/ppxb/miyabi/internal/javbus"
	"github.com/ppxb/miyabi/internal/javdb"
	"github.com/ppxb/miyabi/internal/magnet"
	"github.com/ppxb/miyabi/internal/netx"
)

const (
	javdbDeviceSetting   = "javdb.device_uuid"
	javdbRouteSetting    = "javdb.route"
	javbusEnabledSetting = "magnet.javbus.enabled"
	javbusBaseURLSetting = "magnet.javbus.base_url"
)

type persistedRoute struct {
	Host      string `json:"host"`
	LatencyMS int64  `json:"latency_ms"`
	Manual    bool   `json:"manual"`
}

// Service combines JavDB catalogue data with Miyabi's local library and workflow state.
type Service struct {
	database   *ent.Client
	local      LocalState
	javdb      Provider
	javbus     *javbus.Client
	aggregator *magnet.Aggregator
	lists      *responseCache[[]domain.Movie]
	details    *responseCache[domain.MovieDetail]
	tags       *responseCache[[]domain.TagCategory]
	magnets    *responseCache[[]domain.Magnet]

	routeMu sync.RWMutex
	route   RouteStatus
}

type dynamicJavBusSource struct {
	client   *javbus.Client
	database *ent.Client
}

func (s *dynamicJavBusSource) Name() string {
	return "javbus"
}

func (s *dynamicJavBusSource) Find(ctx context.Context, ref domain.MovieRef) ([]domain.Magnet, error) {
	if s.client == nil {
		return nil, nil
	}
	if s.database != nil {
		enabled, found, _ := loadSetting[bool](ctx, s.database, javbusEnabledSetting)
		if !found || !enabled {
			return nil, nil
		}
	}
	return s.client.Find(ctx, ref)
}

// New creates the lazy JavDB client and restores/persists device UUID and route settings.
func New(
	ctx context.Context,
	database *ent.Client,
	options javdb.Options,
	proxy *netx.ProxyManager,
	local LocalState,
) (*Service, error) {
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

	javbusBaseURL, _, _ := loadSetting[string](ctx, database, javbusBaseURLSetting)
	javbusClient, err := javbus.New(javbus.Options{
		BaseURL: javbusBaseURL,
		Proxy:   proxy,
	})
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("initialize JavBus client: %w", err)
	}

	javbusSource := &dynamicJavBusSource{client: javbusClient, database: database}
	aggregator := magnet.NewAggregator([]magnet.Source{client, javbusSource}, 8*time.Second, nil)

	return newService(database, client, javbusClient, aggregator, local, route), nil
}

// NewWithProvider creates a catalogue Service with an explicit Provider.
// It is primarily used for testing or alternative catalogue sources.
func NewWithProvider(
	ctx context.Context,
	database *ent.Client,
	provider Provider,
	local LocalState,
) (*Service, error) {
	route, _, err := loadSetting[persistedRoute](ctx, database, javdbRouteSetting)
	if err != nil {
		return nil, err
	}
	var agg *magnet.Aggregator
	if src, ok := provider.(magnet.Source); ok {
		agg = magnet.NewAggregator([]magnet.Source{src}, 8*time.Second, nil)
	}
	return newService(database, provider, nil, agg, local, route), nil
}

// Response caches absorb repeated page loads; entries are small and short-lived
// so JavDB changes (new magnets, edited metadata) surface within minutes.
const (
	listCacheSize, listCacheTTL       = 128, time.Minute
	detailCacheSize, detailCacheTTL   = 256, 5 * time.Minute
	tagsCacheSize, tagsCacheTTL       = 5, 24 * time.Hour
	magnetsCacheSize, magnetsCacheTTL = 64, time.Minute
)

func newService(
	database *ent.Client,
	provider Provider,
	javbusClient *javbus.Client,
	aggregator *magnet.Aggregator,
	local LocalState,
	route persistedRoute,
) *Service {
	return &Service{
		database:   database,
		javdb:      provider,
		javbus:     javbusClient,
		aggregator: aggregator,
		lists:      newResponseCache[[]domain.Movie](listCacheSize, listCacheTTL),
		details:    newResponseCache[domain.MovieDetail](detailCacheSize, detailCacheTTL),
		tags:       newResponseCache[[]domain.TagCategory](tagsCacheSize, tagsCacheTTL),
		magnets:    newResponseCache[[]domain.Magnet](magnetsCacheSize, magnetsCacheTTL),
		local:      local,
		route: RouteStatus{
			Host:      route.Host,
			LatencyMS: route.LatencyMS,
			Active:    route.Host != "",
			Manual:    route.Manual,
		},
	}
}

func (service *Service) Close() {
	if service.javdb != nil {
		service.javdb.Close()
	}
	if service.javbus != nil {
		service.javbus.Close()
	}
}

func (service *Service) Search(
	ctx context.Context,
	keyword string,
	options domain.SearchOptions,
) ([]Movie, error) {
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

func (service *Service) Browse(
	ctx context.Context,
	options domain.BrowseOptions,
) ([]Movie, error) {
	key := fmt.Sprintf("browse:%#v", options)
	movies, err := cachedJavDB(ctx, service, service.lists, key, func(ctx context.Context) ([]domain.Movie, error) {
		return service.javdb.Browse(ctx, options)
	})
	if err != nil {
		return nil, fmt.Errorf("browse JavDB: %w", err)
	}
	return service.projectMovies(ctx, movies)
}

func (service *Service) CatalogueDetail(ctx context.Context, movieID string) (domain.MovieDetail, error) {
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

func (service *Service) MovieDetail(ctx context.Context, movieID string) (MovieDetail, error) {
	movie, err := service.CatalogueDetail(ctx, movieID)
	if err != nil {
		return MovieDetail{}, fmt.Errorf("get JavDB movie detail: %w", err)
	}
	projected, err := service.projectMovies(ctx, []domain.Movie{movie.Movie})
	if err != nil {
		return MovieDetail{}, err
	}
	return MovieDetail{
		Movie:         projected[0],
		Zone:          movie.Zone,
		ActorMovies:   movie.ActorMovies,
		RelatedMovies: movie.RelatedMovies,
	}, nil
}

func (service *Service) Magnets(ctx context.Context, movieID string) ([]Magnet, error) {
	magnets, err := cachedJavDB(ctx, service, service.magnets, movieID, func(ctx context.Context) ([]domain.Magnet, error) {
		if service.aggregator != nil {
			var code string
			var zone domain.Zone
			if d, err := service.CatalogueDetail(ctx, movieID); err == nil {
				code = d.Code
				zone = d.Zone
			}
			return service.aggregator.Find(ctx, domain.MovieRef{
				Code:    code,
				JavDBID: movieID,
				Zone:    zone,
			})
		}
		return service.javdb.Magnets(ctx, movieID)
	})
	if err != nil {
		return nil, fmt.Errorf("get magnets: %w", err)
	}
	return projectMagnets(magnets), nil
}

func (service *Service) HasMagnet(ctx context.Context, movieID, hash string) (bool, error) {
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

func (service *Service) MovieCode(ctx context.Context, movieID string) (string, error) {
	movie, err := service.CatalogueDetail(ctx, movieID)
	if err != nil {
		return "", err
	}
	return movie.Code, nil
}

func (service *Service) MovieSummary(ctx context.Context, movieID string) (domain.MovieSummary, error) {
	detail, err := service.MovieDetail(ctx, movieID)
	if err != nil {
		return domain.MovieSummary{}, err
	}
	return domain.MovieSummary{
		ID:          detail.ID,
		Code:        detail.Code,
		Title:       detail.Title,
		Cover:       detail.Cover,
		ReleaseDate: detail.ReleaseDate,
	}, nil
}

func (service *Service) FirstMagnetHash(ctx context.Context, movieID string) (string, error) {
	magnets, err := service.Magnets(ctx, movieID)
	if err != nil {
		return "", err
	}
	if len(magnets) == 0 {
		return "", nil
	}
	return strings.ToLower(magnets[0].Hash), nil
}

func (service *Service) Media(ctx context.Context, rawURL string) (domain.Media, error) {
	media, err := service.javdb.FetchMedia(ctx, rawURL)
	if err != nil {
		return domain.Media{}, fmt.Errorf("fetch JavDB media: %w", err)
	}
	return media, nil
}

func (service *Service) Tags(ctx context.Context, zone domain.Zone) ([]domain.TagCategory, error) {
	categories, err := cachedJavDB(ctx, service, service.tags, string(zone), func(ctx context.Context) ([]domain.TagCategory, error) {
		return service.javdb.Tags(ctx, zone)
	})
	if err != nil {
		return nil, fmt.Errorf("get JavDB tags: %w", err)
	}
	return categories, nil
}

func (service *Service) ResolveMovieID(ctx context.Context, code string) (string, error) {
	id, err := service.javdb.ResolveMovieID(ctx, code)
	if err != nil {
		return "", fmt.Errorf("resolve JavDB movie ID: %w", err)
	}
	if err := service.persistActiveRoute(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (service *Service) Route() RouteStatus {
	status, active := service.javdb.Route()
	result := RouteStatus{
		Host:       status.Host,
		LatencyMS:  status.Latency.Milliseconds(),
		Active:     active,
		Manual:     status.Manual,
		Candidates: make([]RouteCandidate, len(status.Candidates)),
	}
	for index, candidate := range status.Candidates {
		result.Candidates[index] = RouteCandidate{
			Host:      candidate.Host,
			LatencyMS: candidate.Latency.Milliseconds(),
			Status:    candidate.Status,
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

func (service *Service) SelectRoute(ctx context.Context, host string) (RouteStatus, error) {
	if host == "" {
		return service.Reselect(ctx)
	}
	if _, err := service.javdb.SelectRoute(ctx, host); err != nil {
		return RouteStatus{}, fmt.Errorf("select JavDB route: %w", err)
	}
	if err := service.persistActiveRoute(ctx); err != nil {
		return RouteStatus{}, err
	}
	return service.Route(), nil
}

func (service *Service) Reselect(ctx context.Context) (RouteStatus, error) {
	if _, err := service.javdb.Reselect(ctx); err != nil {
		return RouteStatus{}, fmt.Errorf("reselect JavDB route: %w", err)
	}
	if err := service.persistActiveRoute(ctx); err != nil {
		return RouteStatus{}, err
	}
	return service.Route(), nil
}

// Facets exposes available zones and sort options for front-end discovery filtering.
func (service *Service) Facets() Facets {
	return Facets{
		Zones: []FacetItem{
			{Value: string(domain.ZoneCensored), Label: "有码"},
			{Value: string(domain.ZoneUncensored), Label: "无码"},
			{Value: string(domain.ZoneFC2), Label: "FC2"},
			{Value: string(domain.ZoneWestern), Label: "欧美"},
			{Value: string(domain.ZoneAnime), Label: "动漫"},
		},
		Sorts: []FacetItem{
			{Value: "release", Label: "发布日期"},
			{Value: "update", Label: "更新日期"},
			{Value: "hit", Label: "热度"},
			{Value: "score", Label: "评分"},
		},
	}
}

func (service *Service) projectMovies(
	ctx context.Context,
	source []domain.Movie,
) ([]Movie, error) {
	if len(source) == 0 {
		return []Movie{}, nil
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
	result := make([]Movie, len(source))
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
		result[index] = Movie{
			Movie:         item,
			LibraryID:     states[index].LibraryID,
			State:         states[index].State,
			ReleaseStatus: releaseStatus,
		}
	}
	return result, nil
}

func (service *Service) persistActiveRoute(ctx context.Context) error {
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
	service.route = RouteStatus{
		Host:      route.Host,
		LatencyMS: route.LatencyMS,
		Active:    true,
		Manual:    route.Manual,
	}
	return nil
}

func projectMagnets(source []domain.Magnet) []Magnet {
	result := make([]Magnet, len(source))
	for index, item := range source {
		result[index] = Magnet{Magnet: item, URI: "magnet:?xt=urn:btih:" + item.Hash}
	}
	return result
}

func loadSetting[T any](ctx context.Context, database *ent.Client, key string) (T, bool, error) {
	var value T
	if database == nil {
		return value, false, nil
	}
	record, err := database.Setting.Query().Where(setting.Key(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return value, false, nil
	}
	if err != nil {
		return value, false, fmt.Errorf("load setting %s: %w", key, err)
	}
	if err := json.Unmarshal(record.Value, &value); err != nil {
		return value, false, fmt.Errorf("decode setting %s: %w", key, err)
	}
	return value, true, nil
}

func saveSetting(ctx context.Context, database *ent.Client, key string, value any) error {
	if database == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode setting %s: %w", key, err)
	}
	if err := database.Setting.Create().SetKey(key).SetValue(jsontext.Value(encoded)).
		OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}
	return nil
}
