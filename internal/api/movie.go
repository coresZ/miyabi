package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
	lib "github.com/ppxb/miyabi/internal/library"
	"github.com/ppxb/miyabi/internal/tasks"
)

type LibraryManager interface {
	ViewedManager
	Movies(context.Context, int, int) (lib.Page, error)
	MarkWatched(context.Context, int, domain.WatchHistoryScope) (lib.WatchSession, error)
	WatchHistory(context.Context, int) (lib.WatchHistoryPage, error)
	SaveWatchProgress(context.Context, int, lib.WatchProgress) error
	RemoveWatchHistory(context.Context, domain.WatchHistoryScope, []int) (int, error)
	ClearWatchHistory(context.Context, domain.WatchHistoryScope) (int, error)
	StartScan(context.Context) (tasks.TaskInfo, error)
}

type ArtworkReader interface {
	Artwork(string) ([]byte, error)
}

func libraryArtworkHandler(artwork ArtworkReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			Key string `uri:"key" binding:"required,len=64,hexadecimal"`
		}](c)
		if !ok {
			return
		}
		body, err := artwork.Artwork(uri.Key)
		if err != nil {
			c.Error(err)
			return
		}
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Data(http.StatusOK, "image/jpeg", body)
	}
}

type libraryPageQuery struct {
	Page  int `form:"page,default=1" binding:"min=1"`
	Limit int `form:"limit,default=20" binding:"min=1,max=100"`
}

func libraryMoviesHandler(library LibraryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		query, ok := bindQuery[libraryPageQuery](c)
		if !ok {
			return
		}
		movies, err := library.Movies(c.Request.Context(), query.Page, query.Limit)
		respond(c, movies, err)
	}
}

func libraryWatchedHandler(library LibraryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID int `uri:"id" binding:"required,min=1"`
		}](c)
		if !ok {
			return
		}
		scope, ok := bindJSON[domain.WatchHistoryScope](c)
		if !ok {
			return
		}
		history, err := library.MarkWatched(c.Request.Context(), uri.ID, scope)
		respond(c, gin.H{"id": uri.ID, "watched": true, "history": history}, err)
	}
}

func libraryScanHandler(library LibraryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		task, err := library.StartScan(c.Request.Context())
		accepted(c, task, err)
	}
}
