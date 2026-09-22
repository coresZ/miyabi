package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/netx"
)

type NetworkManager interface {
	Network(context.Context) (netx.ProxyConfig, error)
	UpdateNetwork(context.Context, netx.ProxyConfig) error
	TestNetwork(context.Context, netx.ProxyConfig) (netx.NetworkTestResponse, error)
}


func networkHandler(network NetworkManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, err := network.Network(c.Request.Context())
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, config)
	}
}

func networkUpdateHandler(network NetworkManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var config netx.ProxyConfig
		if err := c.ShouldBindJSON(&config); err != nil {
			c.Error(BadRequest(err))
			return
		}
		if err := network.UpdateNetwork(c.Request.Context(), config); err != nil {
			c.Error(err)
			return
		}
		config, err := network.Network(c.Request.Context())
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, config)
	}
}

// networkTestHandler probes with the configuration in the request body, or
// with the saved configuration when the body is empty.
func networkTestHandler(network NetworkManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, err := network.Network(c.Request.Context())
		if err != nil {
			c.Error(err)
			return
		}
		if c.Request.ContentLength != 0 {
			if err := c.ShouldBindJSON(&config); err != nil {
				c.Error(BadRequest(err))
				return
			}
		}
		result, err := network.TestNetwork(c.Request.Context(), config)
		if err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
