package api

import (
	"context"

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
		respond(c, items, err)
	}
}

func monitorAddHandler(monitors MonitorManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		input, ok := bindJSON[monitorInput](c)
		if !ok {
			return
		}
		item, err := monitors.Add(c.Request.Context(), input.MovieID)
		created(c, item, err)
	}
}

func monitorRemoveHandler(monitors MonitorManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[movieURI](c)
		if !ok {
			return
		}
		err := monitors.Remove(c.Request.Context(), uri.ID)
		respond(c, nil, err)
	}
}

func monitorRetryHandler(monitors MonitorManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[movieURI](c)
		if !ok {
			return
		}
		item, err := monitors.Retry(c.Request.Context(), uri.ID)
		respond(c, item, err)
	}
}
