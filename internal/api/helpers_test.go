package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type testPayload struct {
	Name string `json:"name" form:"name" uri:"name" binding:"required"`
}

func TestHelpersBindingAndResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("bindJSON success and respond", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := gin.New()
		r.POST("/test", func(c *gin.Context) {
			input, ok := bindJSON[testPayload](c)
			if !ok {
				return
			}
			respond(c, gin.H{"echo": input.Name}, nil)
		})

		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"name":"alice"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if body := w.Body.String(); body != `{"echo":"alice"}` {
			t.Fatalf("unexpected body: %s", body)
		}
	})

	t.Run("bindJSON validation error", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := gin.New()
		var errCount int
		r.Use(func(c *gin.Context) {
			c.Next()
			errCount = len(c.Errors)
		})
		r.POST("/test", func(c *gin.Context) {
			_, ok := bindJSON[testPayload](c)
			if !ok {
				return
			}
			respond(c, "ok", nil)
		})

		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if errCount == 0 {
			t.Fatal("expected error attached to context")
		}
	})

	t.Run("bindQuery and accepted", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := gin.New()
		r.GET("/test", func(c *gin.Context) {
			query, ok := bindQuery[testPayload](c)
			if !ok {
				return
			}
			accepted(c, gin.H{"accepted": query.Name}, nil)
		})

		req := httptest.NewRequest(http.MethodGet, "/test?name=bob", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d", w.Code)
		}
	})

	t.Run("bindURI and created", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := gin.New()
		r.GET("/test/:name", func(c *gin.Context) {
			uri, ok := bindURI[testPayload](c)
			if !ok {
				return
			}
			created(c, gin.H{"created": uri.Name}, nil)
		})

		req := httptest.NewRequest(http.MethodGet, "/test/charlie", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}
	})

	t.Run("respond with error attaches error", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := gin.New()
		var errCount int
		r.Use(func(c *gin.Context) {
			c.Next()
			errCount = len(c.Errors)
		})
		r.GET("/test", func(c *gin.Context) {
			respond(c, nil, errors.New("boom"))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		r.ServeHTTP(w, req)

		if errCount == 0 {
			t.Fatal("expected error attached")
		}
	})

	t.Run("noStore middleware sets header", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := gin.New()
		r.GET("/test", noStore(), func(c *gin.Context) {
			respond(c, "ok", nil)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		r.ServeHTTP(w, req)

		if header := w.Header().Get("Cache-Control"); header != "no-store" {
			t.Fatalf("expected Cache-Control: no-store, got %q", header)
		}
	})
}
