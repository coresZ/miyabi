package domain

import (
	"path"
	"strings"
)

// MinVideoSize is the minimum file size to qualify for video identification (100MB).
const MinVideoSize int64 = 100 << 20

// IsVideo reports whether a filename has a recognized video extension.
func IsVideo(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".ts", ".m2ts", ".mts", ".mpg", ".mpeg", ".vob":
		return true
	default:
		return false
	}
}
