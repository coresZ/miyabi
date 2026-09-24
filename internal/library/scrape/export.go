package scrape

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/ent"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

// ExportEmbyMedia writes .strm, .nfo, poster.jpg, and fanart.jpg files to the Emby directory structure.
func ExportEmbyMedia(embyDir, publicURL, strmToken, code string, doc nfo.Movie, videos []pan.File, poster, fanart []byte) error {
	if embyDir == "" {
		embyDir = "./data/emby"
	}
	if publicURL == "" {
		publicURL = "http://127.0.0.1:8080"
	}

	stem := nfo.FileStem(code)
	prefix := codeid.Prefix(code)
	destDir := filepath.Join(embyDir, prefix, code)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create emby directory %s: %w", destDir, err)
	}

	tokenParam := ""
	if strmToken != "" {
		tokenParam = "?token=" + url.QueryEscape(strmToken)
	}

	// 1. Write STRM files
	if len(videos) == 1 {
		strmPath := filepath.Join(destDir, stem+".strm")
		content := fmt.Sprintf("%s/api/strm/play/%s%s\n", publicURL, videos[0].ID, tokenParam)
		if err := os.WriteFile(strmPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write strm file: %w", err)
		}
	} else if len(videos) > 1 {
		for i, v := range videos {
			strmPath := filepath.Join(destDir, fmt.Sprintf("%s-cd%d.strm", stem, i+1))
			content := fmt.Sprintf("%s/api/strm/play/%s%s\n", publicURL, v.ID, tokenParam)
			if err := os.WriteFile(strmPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write strm file: %w", err)
			}
		}
	}

	// 2. Write NFO file
	posterName, fanartName := "poster.jpg", "fanart.jpg"
	nfoName := stem + ".nfo"
	doc.Thumbs = []nfo.Thumb{{Aspect: "poster", Path: posterName}}
	doc.Fanart = fanartName
	nfoBody, err := nfo.Encode(doc)
	if err != nil {
		return fmt.Errorf("encode nfo: %w", err)
	}
	nfoPath := filepath.Join(destDir, nfoName)
	if err := os.WriteFile(nfoPath, nfoBody, 0o644); err != nil {
		return fmt.Errorf("write nfo file: %w", err)
	}

	// 3. Write poster and fanart
	if len(poster) > 0 {
		posterPath := filepath.Join(destDir, posterName)
		if err := os.WriteFile(posterPath, poster, 0o644); err != nil {
			return fmt.Errorf("write poster: %w", err)
		}
	}
	if len(fanart) > 0 {
		fanartPath := filepath.Join(destDir, fanartName)
		if err := os.WriteFile(fanartPath, fanart, 0o644); err != nil {
			return fmt.Errorf("write fanart: %w", err)
		}
	}

	return nil
}

// ExportLocalMovie exports an already-scraped ent.Movie record and cached artwork to the Emby directory if missing.
func ExportLocalMovie(embyDir, publicURL, strmToken string, record *ent.Movie, images *mediaimage.Cache) error {
	if record == nil || record.Code == "" {
		return nil
	}
	if embyDir == "" {
		embyDir = "./data/emby"
	}

	prefix := codeid.Prefix(record.Code)
	destDir := filepath.Join(embyDir, prefix, record.Code)
	stem := nfo.FileStem(record.Code)
	nfoPath := filepath.Join(destDir, stem+".nfo")
	posterPath := filepath.Join(destDir, "poster.jpg")
	strmPath := filepath.Join(destDir, stem+".strm")

	// If .nfo and poster.jpg already exist, and at least one strm exists, we don't need to re-write.
	_, nfoErr := os.Stat(nfoPath)
	_, posterErr := os.Stat(posterPath)
	_, strmErr := os.Stat(strmPath)
	if nfoErr == nil && posterErr == nil && (strmErr == nil || len(record.Edges.Files) > 1) {
		return nil
	}

	doc := MovieNFO(record)
	videos := make([]pan.File, 0, len(record.Edges.Files))
	for _, f := range record.Edges.Files {
		videos = append(videos, pan.File{ID: f.FileID, Name: f.Name, Size: f.Size, PickCode: f.PickCode})
	}

	var posterBytes, fanartBytes []byte
	if images != nil {
		artwork := MovieArtwork(record)
		if artwork.Poster != "" {
			posterBytes, _ = images.ReadURL(artwork.Poster)
		}
		if artwork.Fanart != "" {
			fanartBytes, _ = images.ReadURL(artwork.Fanart)
		} else if artwork.Thumbnail != "" {
			fanartBytes, _ = images.ReadURL(artwork.Thumbnail)
		}
	}

	return ExportEmbyMedia(embyDir, publicURL, strmToken, record.Code, doc, videos, posterBytes, fanartBytes)
}
