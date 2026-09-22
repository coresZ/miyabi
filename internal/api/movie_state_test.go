package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/catalogue"
)

type movieStateStub struct {
	Discoverer
	movies []catalogue.MovieIdentity
}

func (stub *movieStateStub) MovieStates(_ context.Context, movies []catalogue.MovieIdentity) ([]catalogue.MovieStateItem, error) {
	stub.movies = movies
	return []catalogue.MovieStateItem{{ID: movies[0].ID, State: catalogue.MovieInLibrary, LibraryID: 42}}, nil
}


func TestMovieStatesHandlerValidatesBatchesAndDisablesHTTPCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := catalogue.MovieIdentity{ID: "catalogue-id", Code: "ABP-001"}
	tooMany := make([]catalogue.MovieIdentity, 101)
	for index := range tooMany {
		tooMany[index] = valid
	}
	for _, scenario := range []struct {
		name   string
		movies []catalogue.MovieIdentity
		valid  bool
	}{
		{"valid batch", []catalogue.MovieIdentity{valid}, true},
		{"empty batch", []catalogue.MovieIdentity{}, false},
		{"missing id", []catalogue.MovieIdentity{{Code: valid.Code}}, false},
		{"missing code", []catalogue.MovieIdentity{{ID: valid.ID}}, false},
		{"oversized batch", tooMany, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body, err := json.Marshal(movieStatesInput{Movies: scenario.movies})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/discover/movie-states", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			stub := &movieStateStub{}
			discoverMovieStatesHandler(stub)(c)
			if !scenario.valid {
				if len(c.Errors) != 1 || stub.movies != nil {
					t.Fatalf("invalid batch reached the service: errors=%v movies=%v", c.Errors, stub.movies)
				}
				return
			}
			var states []catalogue.MovieStateItem
			if err := json.Unmarshal(response.Body.Bytes(), &states); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" ||
				len(states) != 1 || states[0].ID != valid.ID || states[0].LibraryID != 42 ||
				len(stub.movies) != 1 || stub.movies[0] != valid {
				t.Fatalf("incorrect state response: %s, movies=%v", response.Body, stub.movies)
			}
		})
	}
}

