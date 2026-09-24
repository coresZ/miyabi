package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// DesktopHooks exposes desktop-shell actions to the local HTTP API. It is nil
// when the application runs as the console server or inside Docker, in which
// case the desktop routes are not registered.
type DesktopHooks interface {
	RevealDataDir() error
	Quit()
}

func desktopRevealDataDirHandler(hooks DesktopHooks) gin.HandlerFunc {
	return func(c *gin.Context) {
		respond(c, gin.H{"success": true}, hooks.RevealDataDir())
	}
}

func desktopQuitHandler(hooks DesktopHooks) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusAccepted, gin.H{"success": true})
		// Quit tears down the whole application, so run it after the response
		// has been written and let the runtime drive the shutdown.
		go hooks.Quit()
	}
}
