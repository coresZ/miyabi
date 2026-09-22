package api

import (
	"context"
	"net/http"

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
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, info)
	}
}

func dataClearCacheHandler(data MaintenanceManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		info, err := data.ClearCache(c.Request.Context())
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, info)
	}
}
