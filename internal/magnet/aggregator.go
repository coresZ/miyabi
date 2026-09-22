package magnet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

const (
	defaultAggregatorTimeout = 8 * time.Second
)

// Aggregator coordinates concurrent magnet queries across multiple sources,
// deduplicates by infohash, merges metadata, enriches quality tags, and orders results.
type Aggregator struct {
	sources []Source
	timeout time.Duration
	logger  *slog.Logger
}

// NewAggregator creates an Aggregator.
func NewAggregator(sources []Source, timeout time.Duration, logger *slog.Logger) *Aggregator {
	if timeout <= 0 {
		timeout = defaultAggregatorTimeout
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Aggregator{
		sources: sources,
		timeout: timeout,
		logger:  logger,
	}
}

type queryResult struct {
	sourceName string
	magnets    []domain.Magnet
	err        error
}

// Find executes concurrent magnet searches across all sources and merges results.
func (a *Aggregator) Find(ctx context.Context, ref domain.MovieRef) ([]domain.Magnet, error) {
	if len(a.sources) == 0 {
		return nil, nil
	}

	results := make([]queryResult, len(a.sources))
	var wg sync.WaitGroup

	for i, s := range a.sources {
		wg.Add(1)
		go func(idx int, src Source) {
			defer wg.Done()
			sourceCtx, cancel := context.WithTimeout(ctx, a.timeout)
			defer cancel()

			magnets, err := src.Find(sourceCtx, ref)
			results[idx] = queryResult{
				sourceName: src.Name(),
				magnets:    magnets,
				err:        err,
			}
		}(i, s)
	}

	wg.Wait()

	var successCount int
	var allErrors []error
	merged := make(map[string]*domain.Magnet)
	var order []string // preserve insertion order for determinism

	for _, res := range results {
		if res.err != nil {
			a.logger.WarnContext(ctx, "magnet source query failed",
				"source", res.sourceName,
				"code", ref.Code,
				"error", res.err,
			)
			allErrors = append(allErrors, fmt.Errorf("%s: %w", res.sourceName, res.err))
			continue
		}

		successCount++
		for _, m := range res.magnets {
			hash := strings.ToLower(strings.TrimSpace(m.Hash))
			if hash == "" {
				continue
			}

			if existing, exists := merged[hash]; exists {
				mergeMagnet(existing, m, res.sourceName)
			} else {
				entry := m
				entry.Hash = hash
				if len(entry.Sources) == 0 {
					entry.Sources = []string{res.sourceName}
				}
				merged[hash] = &entry
				order = append(order, hash)
			}
		}
	}

	if successCount == 0 && len(allErrors) > 0 {
		return nil, domain.E(domain.KindUpstream, "all magnet sources failed", errors.Join(allErrors...))
	}

	result := make([]domain.Magnet, 0, len(merged))
	for _, hash := range order {
		item := merged[hash]
		ApplyInference(item)
		result = append(result, *item)
	}

	// Order results: Subtitle > HD > Size > FilesCount > JavDB source > CreatedAt
	slices.SortStableFunc(result, func(a, b domain.Magnet) int {
		if a.HasSubtitle != b.HasSubtitle {
			if a.HasSubtitle {
				return -1
			}
			return 1
		}
		if a.HD != b.HD {
			if a.HD {
				return -1
			}
			return 1
		}
		if a.Size != b.Size {
			if a.Size > b.Size {
				return -1
			}
			return 1
		}
		if a.FilesCount != b.FilesCount {
			if a.FilesCount > b.FilesCount {
				return -1
			}
			return 1
		}
		hasJavDBA := slices.Contains(a.Sources, "javdb")
		hasJavDBB := slices.Contains(b.Sources, "javdb")
		if hasJavDBA != hasJavDBB {
			if hasJavDBA {
				return -1
			}
			return 1
		}
		if a.CreatedAt != b.CreatedAt {
			if a.CreatedAt > b.CreatedAt {
				return -1
			}
			return 1
		}
		return 0
	})

	return result, nil
}

func mergeMagnet(existing *domain.Magnet, incoming domain.Magnet, sourceName string) {
	// 1. Sources: keep unique, prefer javdb at the beginning if present
	if !slices.Contains(existing.Sources, sourceName) {
		if sourceName == "javdb" {
			existing.Sources = append([]string{"javdb"}, existing.Sources...)
		} else {
			existing.Sources = append(existing.Sources, sourceName)
		}
	}
	for _, src := range incoming.Sources {
		if !slices.Contains(existing.Sources, src) {
			if src == "javdb" {
				existing.Sources = append([]string{"javdb"}, existing.Sources...)
			} else {
				existing.Sources = append(existing.Sources, src)
			}
		}
	}

	// 2. Flags
	existing.HasSubtitle = existing.HasSubtitle || incoming.HasSubtitle
	existing.HD = existing.HD || incoming.HD

	// 3. Size and files: take maximum non-zero
	if incoming.Size > existing.Size {
		existing.Size = incoming.Size
	}
	if incoming.FilesCount > existing.FilesCount {
		existing.FilesCount = incoming.FilesCount
	}

	// 4. Name: prefer javdb cleaned name if either is from javdb, else longer
	if slices.Contains(incoming.Sources, "javdb") || sourceName == "javdb" {
		if incoming.Name != "" {
			existing.Name = incoming.Name
		}
	} else if len(incoming.Name) > len(existing.Name) {
		existing.Name = incoming.Name
	}

	// 5. CreatedAt: take earliest non-empty date
	if existing.CreatedAt == "" {
		existing.CreatedAt = incoming.CreatedAt
	} else if incoming.CreatedAt != "" && incoming.CreatedAt < existing.CreatedAt {
		existing.CreatedAt = incoming.CreatedAt
	}

	// 6. Tags: merge unique
	for _, t := range incoming.Tags {
		if !slices.Contains(existing.Tags, t) {
			existing.Tags = append(existing.Tags, t)
		}
	}
}
