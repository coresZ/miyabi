package api

import (
	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/catalogue"
)

func javbusConfigHandler(discover CatalogueManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, err := discover.JavBus(c.Request.Context())
		respond(c, config, err)
	}
}

func javbusUpdateHandler(discover CatalogueManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, ok := bindJSON[catalogue.JavBusConfig](c)
		if !ok {
			return
		}
		if err := discover.UpdateJavBus(c.Request.Context(), config); err != nil {
			c.Error(err)
			return
		}
		updated, err := discover.JavBus(c.Request.Context())
		respond(c, updated, err)
	}
}
