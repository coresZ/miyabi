package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"
)

type HealthChecker interface {
	Ping(context.Context) error
}

type Dependencies struct {
	Logger      *slog.Logger
	Health      HealthChecker
	Access      AccessGate
	Catalogue   CatalogueManager
	Drive       DriveManager
	Offline     OfflineManager
	Monitor     SubscriptionManager
	Library     LibraryManager
	Play        PlayManager
	Tasks       TaskManager
	Artwork     ArtworkReader
	Maintenance MaintenanceManager
	Network     NetworkManager
	Frontend    fs.FS
}

func NewRouter(deps Dependencies) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(
		sloggin.NewWithFilters(deps.Logger, sloggin.IgnoreStatus(statusClientClosedRequest)),
		recoveryMiddleware(deps.Logger),
		errorMiddleware(deps.Logger),
	)

	api := router.Group("/api")
	api.GET("/health", healthHandler(deps.Health))
	authAPI := api.Group("/auth", noStore())
	authAPI.GET("/config", accessConfigHandler(deps.Access))
	authAPI.POST("/login", accessLoginHandler(deps.Access))
	settingsAPI := api.Group("/settings", noStore())
	settingsAPI.GET("/system", dataInfoHandler(deps.Maintenance))
	settingsAPI.DELETE("/cache", dataClearCacheHandler(deps.Maintenance))
	settingsAPI.GET("/network", networkHandler(deps.Network))
	settingsAPI.PUT("/network", networkUpdateHandler(deps.Network))
	settingsAPI.POST("/network/test", networkTestHandler(deps.Network))
	settingsAPI.GET("/javbus", javbusConfigHandler(deps.Catalogue))
	settingsAPI.PUT("/javbus", javbusUpdateHandler(deps.Catalogue))
	settingsAPI.GET("/subscription", subscriptionSettingsGetHandler(deps.Monitor))
	settingsAPI.PUT("/subscription", subscriptionSettingsUpdateHandler(deps.Monitor))
	api.GET("/library/movies", libraryMoviesHandler(deps.Library))
	api.PUT("/library/movies/:id/watched", libraryWatchedHandler(deps.Library))
	api.GET("/library/history", libraryHistoryHandler(deps.Library))
	api.POST("/library/history/remove", libraryHistoryRemoveHandler(deps.Library))
	api.DELETE("/library/history", libraryHistoryClearHandler(deps.Library))
	api.PUT("/library/history/:id/progress", libraryHistoryProgressHandler(deps.Library))
	api.POST("/library/scan", libraryScanHandler(deps.Library))
	api.GET("/library/artwork/:key", libraryArtworkHandler(deps.Artwork))
	playAPI := api.Group("/play", noStore())
	playAPI.GET("/files", playFilesHandler(deps.Play))
	playAPI.GET("/:id", playStartHandler(deps.Play))
	playAPI.DELETE("/:id", playReleaseHandler(deps.Play))
	playAPI.GET("/:id/stream/:resource", playStreamHandler(deps.Play))
	playAPI.HEAD("/:id/stream/:resource", playStreamHandler(deps.Play))
	api.GET("/tasks", noStore(), tasksHandler(deps.Tasks))
	api.GET("/tasks/events", taskEventsHandler(deps.Tasks))
	api.GET("/offline/tasks", noStore(), offlineActivityHandler(deps.Offline))
	subscriptionsAPI := api.Group("/subscriptions", noStore())
	subscriptionsAPI.GET("", subscriptionListHandler(deps.Monitor))
	subscriptionsAPI.POST("", subscriptionCreateHandler(deps.Monitor))
	subscriptionsAPI.PATCH("/:id", subscriptionUpdateHandler(deps.Monitor))
	subscriptionsAPI.DELETE("/:id", subscriptionRemoveHandler(deps.Monitor))
	subscriptionsAPI.POST("/:id/enqueue", subscriptionEnqueueSingleHandler(deps.Monitor))
	subscriptionsAPI.POST("/enqueue", subscriptionEnqueueBatchHandler(deps.Monitor))
	subscriptionsAPI.GET("/actors/:id/feed", subscriptionActorFeedHandler(deps.Monitor))
	api.GET("/discover/movies", discoverBrowseHandler(deps.Catalogue))
	api.POST("/discover/movie-states", noStore(), discoverMovieStatesHandler(deps.Catalogue))
	api.GET("/discover/viewed", noStore(), discoverViewedHandler(deps.Library))
	api.POST("/discover/viewed", discoverAddViewedHandler(deps.Library))
	api.GET("/discover/search", discoverSearchHandler(deps.Catalogue))
	api.GET("/discover/tags", discoverTagsHandler(deps.Catalogue))
	api.GET("/discover/movies/:id", discoverMovieHandler(deps.Catalogue))
	api.GET("/discover/movies/:id/magnets", discoverMagnetsHandler(deps.Catalogue))
	api.POST("/discover/movies/:id/offline", offlineAddHandler(deps.Offline))
	api.GET("/discover/movies/:id/offline", noStore(), offlineTasksHandler(deps.Offline))
	api.GET("/image", imageHandler(deps.Catalogue))
	api.GET("/javdb/route", javdbRouteHandler(deps.Catalogue))
	api.PUT("/javdb/route", javdbSelectRouteHandler(deps.Catalogue))
	api.POST("/javdb/reselect", javdbReselectHandler(deps.Catalogue))
	panAPI := api.Group("/pan", noStore())
	panAPI.GET("/account", panAccountHandler(deps.Drive))
	panAPI.DELETE("/account", panDisconnectHandler(deps.Drive))
	panAPI.POST("/login", panBeginLoginHandler(deps.Drive))
	panAPI.GET("/login/:id", panLoginStatusHandler(deps.Drive))
	panAPI.GET("/files", panFilesHandler(deps.Drive))
	panAPI.PUT("/directory", panSelectDirectoryHandler(deps.Drive))
	panAPI.DELETE("/directory", panClearDirectoryHandler(deps.Drive))

	if deps.Frontend != nil {
		installFrontend(router, deps.Frontend)
	}
	return router
}

func installFrontend(router *gin.Engine, frontend fs.FS) {
	fileServer := http.FileServer(http.FS(frontend))
	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		requestedPath := strings.TrimPrefix(c.Request.URL.Path, "/")
		if requestedPath != "" {
			if _, err := fs.Stat(frontend, requestedPath); err == nil {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
