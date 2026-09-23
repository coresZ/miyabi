package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
)

// SubtitleManager defines the subtitle management interface consumed by the API layer.
type SubtitleManager interface {
	Search(ctx context.Context, code string, isUncensored bool) ([]domain.SubtitleCandidate, error)
	ApplyCandidate(ctx context.Context, movieID int, candidate domain.SubtitleCandidate) (*domain.SubtitleTrack, error)
	GetTrackVTT(ctx context.Context, id int) ([]byte, error)
	UpdateOffset(ctx context.Context, id int, offsetMs int) error
	SetDefault(ctx context.Context, movieID int, subID int) error
	Delete(ctx context.Context, id int) error
}

func subtitleVTTHandler(subtitles SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID string `uri:"id" binding:"required"`
		}](c)
		if !ok {
			return
		}
		rawID := strings.TrimSuffix(uri.ID, ".vtt")
		id, err := strconv.Atoi(rawID)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid subtitle ID"})
			return
		}

		data, err := subtitles.GetTrackVTT(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Type", "text/vtt; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/vtt; charset=utf-8", data)
	}
}

func subtitleSearchHandler(subtitles SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		query, ok := bindQuery[struct {
			Code       string `form:"code" binding:"required"`
			Uncensored bool   `form:"uncensored"`
		}](c)
		if !ok {
			return
		}
		results, err := subtitles.Search(c.Request.Context(), query.Code, query.Uncensored)
		respond(c, results, err)
	}
}

func subtitleApplyHandler(subtitles SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, ok := bindJSON[struct {
			MovieID   int                      `json:"movie_id" binding:"required,min=1"`
			Candidate domain.SubtitleCandidate `json:"candidate" binding:"required"`
		}](c)
		if !ok {
			return
		}
		track, err := subtitles.ApplyCandidate(c.Request.Context(), body.MovieID, body.Candidate)
		respond(c, track, err)
	}
}

func subtitleOffsetHandler(subtitles SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID int `uri:"id" binding:"required,min=1"`
		}](c)
		if !ok {
			return
		}
		body, ok := bindJSON[struct {
			OffsetMs int `json:"offset_ms"`
		}](c)
		if !ok {
			return
		}
		err := subtitles.UpdateOffset(c.Request.Context(), uri.ID, body.OffsetMs)
		respond(c, gin.H{"updated": true, "offset_ms": body.OffsetMs}, err)
	}
}

func subtitleSetDefaultHandler(subtitles SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID int `uri:"id" binding:"required,min=1"`
		}](c)
		if !ok {
			return
		}
		body, ok := bindJSON[struct {
			MovieID int `json:"movie_id" binding:"required,min=1"`
		}](c)
		if !ok {
			return
		}
		err := subtitles.SetDefault(c.Request.Context(), body.MovieID, uri.ID)
		respond(c, gin.H{"updated": true}, err)
	}
}

func subtitleDeleteHandler(subtitles SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID int `uri:"id" binding:"required,min=1"`
		}](c)
		if !ok {
			return
		}
		err := subtitles.Delete(c.Request.Context(), uri.ID)
		respond(c, gin.H{"deleted": true}, err)
	}
}
