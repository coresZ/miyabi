package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javdb"
)

type Discoverer interface {
	Search(context.Context, string, domain.SearchOptions) ([]catalogue.Movie, error)
	Browse(context.Context, domain.BrowseOptions) ([]catalogue.Movie, error)
	MovieDetail(context.Context, string) (catalogue.MovieDetail, error)
	MovieStates(context.Context, []catalogue.MovieIdentity) ([]catalogue.MovieStateItem, error)
	Magnets(context.Context, string) ([]catalogue.Magnet, error)
	Tags(context.Context, domain.Zone) ([]domain.TagCategory, error)
	Media(context.Context, string) (javdb.Media, error)
	Route() catalogue.RouteStatus
	Reselect(context.Context) (catalogue.RouteStatus, error)
	SelectRoute(context.Context, string) (catalogue.RouteStatus, error)
}


type ViewedManager interface {
	ViewedMovieIDs(context.Context, ...int) ([]string, error)
	AddViewedMovieIDs(context.Context, []string) error
}

type discoverSearchQuery struct {
	Query string `form:"q" binding:"required"`
	Page  int    `form:"page,default=1" binding:"min=1"`
	Limit int    `form:"limit,default=20" binding:"min=1,max=100"`
}

type discoverBrowseQuery struct {
	Zone       string   `form:"zone" binding:"excluded_with=EntityType,omitempty,oneof=censored uncensored western fc2 anime"`
	EntityType string   `form:"entity_type" binding:"required_with=EntityID,omitempty,oneof=actor series maker director"`
	EntityID   string   `form:"entity_id" binding:"required_with=EntityType"`
	Main       []string `form:"main" binding:"omitempty,dive,oneof=p m c s i v"`
	TagIDs     []string `form:"tag_id" binding:"excluded_with=EntityType,omitempty,dive,required"`
	Year       string   `form:"year" binding:"excluded_with=EntityType,omitempty,len=4,numeric"`
	Month      string   `form:"month" binding:"excluded_with=EntityType,omitempty,oneof=1 2 3 4 5 6 7 8 9 10 11 12"`
	Sort       string   `form:"sort,default=hit" binding:"oneof=hit release score update want_watch_count watched_count"`
	Order      string   `form:"order,default=desc" binding:"oneof=asc desc"`
	Page       int      `form:"page,default=1" binding:"min=1"`
	Limit      int      `form:"limit,default=20" binding:"min=1,max=100"`
}

type discoverTagsQuery struct {
	Zone string `form:"zone,default=censored" binding:"oneof=censored uncensored western fc2 anime"`
}

type movieURI struct {
	ID string `uri:"id" binding:"required"`
}

type movieStatesInput struct {
	Movies []catalogue.MovieIdentity `json:"movies" binding:"required,min=1,max=100,dive"`
}


func discoverMovieStatesHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input movieStatesInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.Error(BadRequest(err))
			return
		}
		states, err := discover.MovieStates(c.Request.Context(), input.Movies)
		if err != nil {
			c.Error(err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, states)
	}
}

type imageQuery struct {
	URL string `form:"url" binding:"required,url"`
}

type javdbRouteInput struct {
	Host string `json:"host" binding:"omitempty,url"`
}

func discoverSearchHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var query discoverSearchQuery
		if err := c.ShouldBindQuery(&query); err != nil {
			c.Error(BadRequest(err))
			return
		}
		movies, err := discover.Search(c.Request.Context(), query.Query, domain.SearchOptions{
			Page:  query.Page,
			Limit: query.Limit,
		})
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, movies)
	}
}

func discoverBrowseHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var query discoverBrowseQuery
		if err := c.ShouldBindQuery(&query); err != nil {
			c.Error(BadRequest(err))
			return
		}
		movies, err := discover.Browse(c.Request.Context(), domain.BrowseOptions{
			Zone:       domain.Zone(query.Zone),
			EntityType: domain.EntityType(query.EntityType),
			EntityID:   query.EntityID,
			Main:       query.Main,
			TagIDs:     query.TagIDs,
			Year:       query.Year,
			Month:      query.Month,
			Sort:       query.Sort,
			Order:      query.Order,
			Page:       query.Page,
			Limit:      query.Limit,
		})
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, movies)
	}
}

func discoverMovieHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri movieURI
		if err := c.ShouldBindUri(&uri); err != nil {
			c.Error(BadRequest(err))
			return
		}
		movie, err := discover.MovieDetail(c.Request.Context(), uri.ID)
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, movie)
	}
}

func discoverTagsHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var query discoverTagsQuery
		if err := c.ShouldBindQuery(&query); err != nil {
			c.Error(BadRequest(err))
			return
		}
		categories, err := discover.Tags(c.Request.Context(), domain.Zone(query.Zone))
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, categories)
	}
}

func discoverMagnetsHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri movieURI
		if err := c.ShouldBindUri(&uri); err != nil {
			c.Error(BadRequest(err))
			return
		}
		magnets, err := discover.Magnets(c.Request.Context(), uri.ID)
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, magnets)
	}
}

func imageHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var query imageQuery
		if err := c.ShouldBindQuery(&query); err != nil {
			c.Error(BadRequest(err))
			return
		}
		media, err := discover.Media(c.Request.Context(), query.URL)
		if err != nil {
			c.Error(err)
			return
		}
		c.Header("Cache-Control", "public, max-age=86400")
		c.Data(http.StatusOK, media.ContentType, media.Body)
	}
}

func javdbRouteHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, discover.Route())
	}
}

func javdbReselectHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, err := discover.Reselect(c.Request.Context())
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, status)
	}
}

func javdbSelectRouteHandler(discover Discoverer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input javdbRouteInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.Error(BadRequest(err))
			return
		}
		status, err := discover.SelectRoute(c.Request.Context(), input.Host)
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, status)
	}
}

type addViewedInput struct {
	IDs []string `json:"ids" binding:"required,min=1,max=1000"`
}

func discoverViewedHandler(viewed ViewedManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		ids, err := viewed.ViewedMovieIDs(c.Request.Context())
		if err != nil {
			c.Error(err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, ids)
	}
}

func discoverAddViewedHandler(viewed ViewedManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input addViewedInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.Error(BadRequest(err))
			return
		}
		if err := viewed.AddViewedMovieIDs(c.Request.Context(), input.IDs); err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, nil)
	}
}
