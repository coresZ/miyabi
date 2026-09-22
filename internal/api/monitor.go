package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/monitor"
)

type MonitorManager interface {
	List(context.Context) ([]monitor.Item, error)
	Add(context.Context, string) (monitor.Item, error)
	Remove(context.Context, string) error
	Retry(context.Context, string) (monitor.Item, error)
}

type monitorInput struct {
	MovieID string `json:"movie_id" binding:"required"`
}

func monitorListHandler(monitors MonitorManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := monitors.List(c.Request.Context())
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, items)
	}
}

func monitorAddHandler(monitors MonitorManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input monitorInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.Error(BadRequest(err))
			return
		}
		item, err := monitors.Add(c.Request.Context(), input.MovieID)
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusCreated, item)
	}
}

func monitorRemoveHandler(monitors MonitorManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri movieURI
		if err := c.ShouldBindUri(&uri); err != nil {
			c.Error(BadRequest(err))
			return
		}
		if err := monitors.Remove(c.Request.Context(), uri.ID); err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, nil)
	}
}

func monitorRetryHandler(monitors MonitorManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri movieURI
		if err := c.ShouldBindUri(&uri); err != nil {
			c.Error(BadRequest(err))
			return
		}
		item, err := monitors.Retry(c.Request.Context(), uri.ID)
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, item)
	}
}
