package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/offline"
)

type OfflineManager interface {
	Add(context.Context, string, string) (domain.OfflineSubmission, error)
	Tasks(context.Context, string, string) ([]domain.OfflineSubmission, error)
	Activity(context.Context) (offline.Activity, error)
}

func offlineActivityHandler(offline OfflineManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		activity, err := offline.Activity(c.Request.Context())
		respond(c, activity, err)
	}
}

type offlineInput struct {
	Hash string `json:"hash" binding:"required,len=40,hexadecimal"`
}

type offlineTasksQuery struct {
	AccountID string `form:"account_id" binding:"required,number"`
}

func offlineTasksHandler(offline OfflineManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[movieURI](c)
		if !ok {
			return
		}
		query, ok := bindQuery[offlineTasksQuery](c)
		if !ok {
			return
		}
		tasks, err := offline.Tasks(c.Request.Context(), uri.ID, query.AccountID)
		respond(c, tasks, err)
	}
}

func offlineAddHandler(offline OfflineManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[movieURI](c)
		if !ok {
			return
		}
		input, ok := bindJSON[offlineInput](c)
		if !ok {
			return
		}
		submission, err := offline.Add(c.Request.Context(), uri.ID, input.Hash)
		accepted(c, submission, err)
	}
}
