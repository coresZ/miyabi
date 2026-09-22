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

const defaultAggregatorTimeout = 8 * time.Second

// Aggregator queries every source concurrently, merges records that share an
// infohash, enriches them with inferred tags, and orders the result.
type Aggregator struct {
	sources []Source
	timeout time.Duration
	logger  *slog.Logger
}

// NewAggregator creates an Aggregator. Each source gets its own timeout; a nil
// logger falls back to the process default.
func NewAggregator(sources []Source, timeout time.Duration, logger *slog.Logger) *Aggregator {
	if timeout <= 0 {
		timeout = defaultAggregatorTimeout
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Aggregator{sources: sources, timeout: timeout, logger: logger}
}

type queryResult struct {
	source  string
	magnets []domain.Magnet
	err     error
}

// Find returns the merged magnets of all sources. One failing source is only
// logged; the call fails when every source failed.
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
			results[idx] = queryResult{source: src.Name(), magnets: magnets, err: err}
		}(i, s)
	}
	wg.Wait()

	var failures []error
	merged := make(map[string]*domain.Magnet)
	var order []string
	for _, res := range results {
		if res.err != nil {
			a.logger.WarnContext(ctx, "magnet source query failed", "source", res.source, "code", ref.Code, "error", res.err)
			failures = append(failures, fmt.Errorf("%s: %w", res.source, res.err))
			continue
		}
		for _, m := range res.magnets {
			hash := strings.ToLower(strings.TrimSpace(m.Hash))
			if hash == "" {
				continue
			}
			if existing, ok := merged[hash]; ok {
				mergeMagnet(existing, m, res.source)
				continue
			}
			entry := m
			entry.Hash = hash
			if len(entry.Sources) == 0 {
				entry.Sources = []string{res.source}
			}
			merged[hash] = &entry
			order = append(order, hash)
		}
	}
	if len(failures) == len(a.sources) {
		return nil, domain.E(domain.KindUpstream, "所有磁力来源均不可用", errors.Join(failures...))
	}

	result := make([]domain.Magnet, 0, len(merged))
	for _, hash := range order {
		item := merged[hash]
		ApplyInference(item)
		result = append(result, *item)
	}

	// 字幕 > 高清 > 体积 > 文件数; JavDB-listed first on ties, then newest.
	slices.SortStableFunc(result, func(a, b domain.Magnet) int {
		if sa, sb := HasSubtitle(a), HasSubtitle(b); sa != sb {
			if sa {
				return -1
			}
			return 1
		}
		if ha, hb := IsHD(a), IsHD(b); ha != hb {
			if ha {
				return -1
			}
			return 1
		}
		return compareMagnets(a, b)
	})
	return result, nil
}

// mergeMagnet folds a second record of the same infohash into existing:
// sources accumulate with JavDB first, flags OR together, size and file count
// take the maximum, the JavDB name wins, and the earliest date is kept.
func mergeMagnet(existing *domain.Magnet, incoming domain.Magnet, source string) {
	existing.Sources = mergeSources(existing.Sources, append([]string{source}, incoming.Sources...))
	existing.HasSubtitle = existing.HasSubtitle || incoming.HasSubtitle
	existing.HD = existing.HD || incoming.HD
	existing.Size = max(existing.Size, incoming.Size)
	existing.FilesCount = max(existing.FilesCount, incoming.FilesCount)

	fromJavDB := source == domain.MagnetSourceJavDB || slices.Contains(incoming.Sources, domain.MagnetSourceJavDB)
	if fromJavDB && incoming.Name != "" {
		existing.Name = incoming.Name
	} else if existing.Name == "" {
		existing.Name = incoming.Name
	}

	if existing.CreatedAt == "" || (incoming.CreatedAt != "" && incoming.CreatedAt < existing.CreatedAt) {
		existing.CreatedAt = incoming.CreatedAt
	}
	for _, tag := range incoming.Tags {
		if !slices.Contains(existing.Tags, tag) {
			existing.Tags = append(existing.Tags, tag)
		}
	}
}

func mergeSources(existing, incoming []string) []string {
	for _, source := range incoming {
		if source == "" || slices.Contains(existing, source) {
			continue
		}
		if source == domain.MagnetSourceJavDB {
			existing = append([]string{source}, existing...)
		} else {
			existing = append(existing, source)
		}
	}
	return existing
}
