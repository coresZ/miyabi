package api

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/playback"
)

type PlayManager interface {
	Files(context.Context, int) (playback.PlayFiles, error)
	Start(context.Context, string) (playback.Playback, error)
	Stream(context.Context, string, int, string, http.Header) (*http.Response, error)
	Release(string)
	StreamURL(context.Context, string) (string, error)
	OpenMedia(context.Context, string, string, http.Header) (*http.Response, error)
}

func playFilesHandler(play PlayManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		query, ok := bindQuery[struct {
			MovieID int `form:"movie_id" binding:"required,min=1"`
		}](c)
		if !ok {
			return
		}
		files, err := play.Files(c.Request.Context(), query.MovieID)
		respond(c, files, err)
	}
}

func playStartHandler(play PlayManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID string `uri:"id" binding:"required,numeric,max=30"`
		}](c)
		if !ok {
			return
		}
		playback, err := play.Start(c.Request.Context(), uri.ID)
		respond(c, playback, err)
	}
}

func playStreamHandler(play PlayManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID       string `uri:"id" binding:"required,uuid"`
			Resource int    `uri:"resource" binding:"min=0"`
		}](c)
		if !ok {
			return
		}
		response, err := play.Stream(c.Request.Context(), uri.ID, uri.Resource, c.Request.Method, c.Request.Header)
		if err != nil {
			c.Error(err)
			return
		}
		defer response.Body.Close()
		for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
			if value := response.Header.Get(name); value != "" {
				c.Header(name, value)
			}
		}
		c.Status(response.StatusCode)
		if c.Request.Method != http.MethodHead {
			if _, err := io.Copy(c.Writer, response.Body); err != nil {
				c.Error(err)
			}
		}
	}
}

func playReleaseHandler(play PlayManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID string `uri:"id" binding:"required,uuid"`
		}](c)
		if !ok {
			return
		}
		play.Release(uri.ID)
		respond(c, gin.H{"released": true}, nil)
	}
}

func playSTRMHandler(play PlayManager, expectedToken string, gate AccessGate) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			FileID string `uri:"fileID" binding:"required"`
		}](c)
		if !ok {
			return
		}
		if expectedToken != "" {
			token := c.Query("token")
			if token != expectedToken {
				if gate != nil && gate.Enabled() {
					jwtToken := extractToken(c)
					if jwtToken == "" {
						c.Error(domain.E(domain.KindUnauthorized, "无效的播放令牌", nil))
						return
					}
					if _, err := gate.VerifyToken(jwtToken); err != nil {
						c.Error(domain.E(domain.KindUnauthorized, "无效的播放令牌", nil))
						return
					}
				} else {
					c.Error(domain.E(domain.KindUnauthorized, "无效的播放令牌", nil))
					return
				}
			}
		} else if gate != nil && gate.Enabled() {
			jwtToken := extractToken(c)
			if jwtToken == "" {
				c.Error(domain.E(domain.KindUnauthorized, "未提供认证令牌，请登录", nil))
				return
			}
			if _, err := gate.VerifyToken(jwtToken); err != nil {
				c.Error(domain.E(domain.KindUnauthorized, "认证令牌无效或已过期", nil))
				return
			}
		}
		streamURL, err := play.StreamURL(c.Request.Context(), uri.FileID)
		if err != nil {
			c.Error(err)
			return
		}
		if c.Request.Method == http.MethodHead {
			response, err := play.OpenMedia(c.Request.Context(), http.MethodHead, streamURL, c.Request.Header)
			if err == nil {
				defer response.Body.Close()
				for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
					if value := response.Header.Get(name); value != "" {
						c.Header(name, value)
					}
				}
				c.Status(response.StatusCode)
				return
			}
		}
		c.Redirect(http.StatusFound, streamURL)
	}
}

