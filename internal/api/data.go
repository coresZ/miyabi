package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/maintenance"
)

type MaintenanceManager interface {
	Info(context.Context) (maintenance.Info, error)
	ClearCache(context.Context) (maintenance.Info, error)
}

func dataInfoHandler(data MaintenanceManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		info, err := data.Info(c.Request.Context())
		respond(c, info, err)
	}
}

func dataClearCacheHandler(data MaintenanceManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		info, err := data.ClearCache(c.Request.Context())
		respond(c, info, err)
	}
}
