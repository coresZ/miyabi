package api

import (
	"github.com/gin-gonic/gin"
)

type AccessGate interface {
	Enabled() bool
	Verify(string) error
}

type accessLoginInput struct {
	Password string `json:"password" binding:"required"`
}

func accessConfigHandler(gate AccessGate) gin.HandlerFunc {
	return func(c *gin.Context) {
		respond(c, gin.H{"enabled": gate.Enabled()}, nil)
	}
}

func accessLoginHandler(gate AccessGate) gin.HandlerFunc {
	return func(c *gin.Context) {
		input, ok := bindJSON[accessLoginInput](c)
		if !ok {
			return
		}
		err := gate.Verify(input.Password)
		respond(c, gin.H{"success": true}, err)
	}
}
