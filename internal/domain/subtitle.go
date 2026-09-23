package domain

// SubtitleTrack represents a subtitle track associated with a movie in the library.
type SubtitleTrack struct {
	ID          int    `json:"id"`
	MovieID     int    `json:"movie_id"`
	FileID      string `json:"file_id,omitempty"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Language    string `json:"language"`
	Format      string `json:"format"`
	VersionTag  string `json:"version_tag"`
	Source      string `json:"source"`
	OffsetMs    int    `json:"offset_ms"`
	IsDefault   bool   `json:"is_default"`
	Src         string `json:"src"`
}

// SubtitleCandidate represents a remote subtitle candidate discovered during search.
type SubtitleCandidate struct {
	Source      string `json:"source"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Language    string `json:"language"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	Ext         string `json:"ext"`
	Score       int    `json:"score"`
}
