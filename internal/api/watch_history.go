package api

import (
	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
	lib "github.com/ppxb/miyabi/internal/library"
)

func libraryHistoryHandler(library LibraryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		query, ok := bindQuery[struct {
			Page int `form:"page,default=1" binding:"min=1,max=100000000"`
		}](c)
		if !ok {
			return
		}
		page, err := library.WatchHistory(c.Request.Context(), query.Page)
		respond(c, page, err)
	}
}

func libraryHistoryProgressHandler(library LibraryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID int `uri:"id" binding:"min=1"`
		}](c)
		if !ok {
			return
		}
		progress, ok := bindJSON[lib.WatchProgress](c)
		if !ok {
			return
		}
		err := library.SaveWatchProgress(c.Request.Context(), uri.ID, progress)
		respond(c, nil, err)
	}
}

func libraryHistoryRemoveHandler(library LibraryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		request, ok := bindJSON[struct {
			domain.WatchHistoryScope
			IDs []int `json:"ids" binding:"required,min=1,max=100,unique,dive,min=1"`
		}](c)
		if !ok {
			return
		}
		count, err := library.RemoveWatchHistory(c.Request.Context(), request.WatchHistoryScope, request.IDs)
		respond(c, gin.H{"removed": count}, err)
	}
}

func libraryHistoryClearHandler(library LibraryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := bindQuery[domain.WatchHistoryScope](c)
		if !ok {
			return
		}
		count, err := library.ClearWatchHistory(c.Request.Context(), scope)
		respond(c, gin.H{"removed": count}, err)
	}
}
