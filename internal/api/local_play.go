package api

import (
	"context"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// LocalPlayer serves indexed local media files without exposing the filesystem.
// The library owns path resolution, root confinement, and file opening.
type LocalPlayer interface {
	LocalPlayFile(context.Context, string) (*os.File, string, error)
}

// localPlayHandler streams one local video with range support so the built-in
// player can seek without proxying the whole file.
func localPlayHandler(player LocalPlayer) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			FileID string `uri:"fileID" binding:"required,max=128"`
		}](c)
		if !ok {
			return
		}
		file, name, err := player.LocalPlayFile(c.Request.Context(), uri.FileID)
		if err != nil {
			c.Error(err)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			c.Error(err)
			return
		}
		c.Header("Cache-Control", "private, max-age=0")
		http.ServeContent(c.Writer, c.Request, name, info.ModTime(), file)
	}
}
