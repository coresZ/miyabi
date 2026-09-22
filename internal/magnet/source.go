package magnet

import (
	"context"

	"github.com/ppxb/miyabi/internal/domain"
)

// Source provides magnets for a given movie reference.
type Source interface {
	Name() string
	Find(ctx context.Context, ref domain.MovieRef) ([]domain.Magnet, error)
}
