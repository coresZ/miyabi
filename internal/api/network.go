package api

import (
	"context"

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
		respond(c, config, err)
	}
}

func networkUpdateHandler(network NetworkManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, ok := bindJSON[netx.ProxyConfig](c)
		if !ok {
			return
		}
		if err := network.UpdateNetwork(c.Request.Context(), config); err != nil {
			c.Error(err)
			return
		}
		updated, err := network.Network(c.Request.Context())
		respond(c, updated, err)
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
			bodyConfig, ok := bindJSON[netx.ProxyConfig](c)
			if !ok {
				return
			}
			config = bodyConfig
		}
		result, err := network.TestNetwork(c.Request.Context(), config)
		respond(c, result, err)
	}
}
