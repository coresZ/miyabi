package subtitle

import "context"

// Provider defines the interface for searching online subtitles.
type Provider interface {
	Name() string
	Search(ctx context.Context, code string) ([]Candidate, error)
}
