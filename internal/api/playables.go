package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/library"
)

// PlayablesManager lists the movies a player can open for one source scope.
type PlayablesManager interface {
	Playables(context.Context, string, string, string, int, int) (library.PlayablePage, error)
}

func playablesHandler(manager PlayablesManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		input, ok := bindQuery[struct {
			Scope     string `form:"scope,default=local"`
			Directory string `form:"directory" binding:"max=4096"`
			Term      string `form:"q" binding:"max=120"`
			Page      int    `form:"page,default=1" binding:"min=1"`
			Limit     int    `form:"limit,default=20" binding:"min=1,max=100"`
		}](c)
		if !ok {
			return
		}
		page, err := manager.Playables(c.Request.Context(), input.Scope, input.Directory, input.Term, input.Page, input.Limit)
		respond(c, page, err)
	}
}
