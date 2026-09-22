package magnet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

type stubSource struct {
	name    string
	magnets []domain.Magnet
	err     error
	delay   time.Duration
}

func (s *stubSource) Name() string {
	return s.name
}

func (s *stubSource) Find(ctx context.Context, ref domain.MovieRef) ([]domain.Magnet, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.magnets, s.err
}

func TestAggregator_DeduplicationAndMerging(t *testing.T) {
	hashCommon := "30291c52bb72d46affc1574ec01a4e16fc28a292"
	hashJavDBOnly := "1111111111111111111111111111111111111111"
	hashJavBusOnly := "2222222222222222222222222222222222222222"

	srcJavDB := &stubSource{
		name: "javdb",
		magnets: []domain.Magnet{
			{
				Hash:        hashCommon,
				Name:        "SSIS-001 Cleaned",
				Size:        2000,
				HasSubtitle: false,
				HD:          true,
				CreatedAt:   "2025-10-30",
				Sources:     []string{"javdb"},
			},
			{
				Hash:        hashJavDBOnly,
				Name:        "SSIS-001 JavDB Only",
				Size:        1000,
				HasSubtitle: true,
				Sources:     []string{"javdb"},
			},
		},
	}

	srcJavBus := &stubSource{
		name: "javbus",
		magnets: []domain.Magnet{
			{
				Hash:        hashCommon,
				Name:        "SSIS-001 Raw",
				Size:        2500, // larger size
				HasSubtitle: true, // has subtitle
				HD:          false,
				CreatedAt:   "2025-10-28", // earlier date
				Sources:     []string{"javbus"},
			},
			{
				Hash:    hashJavBusOnly,
				Name:    "SSIS-001 JavBus Only",
				Size:    5000,
				HD:      true,
				Sources: []string{"javbus"},
			},
		},
	}

	agg := NewAggregator([]Source{srcJavDB, srcJavBus}, time.Second, nil)
	results, err := agg.Find(t.Context(), domain.MovieRef{Code: "SSIS-001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 merged magnets, got %d", len(results))
	}

	// Verify common magnet properties
	var common *domain.Magnet
	for i := range results {
		if results[i].Hash == hashCommon {
			common = &results[i]
			break
		}
	}
	if common == nil {
		t.Fatalf("common magnet not found in results")
	}

	if common.Name != "SSIS-001 Cleaned" {
		t.Errorf("expected javdb name to be preserved, got %s", common.Name)
	}
	if common.Size != 2500 {
		t.Errorf("expected max size 2500, got %d", common.Size)
	}
	if !common.HasSubtitle || !common.HD {
		t.Errorf("expected subtitle and HD both to be true, got sub=%v hd=%v", common.HasSubtitle, common.HD)
	}
	if common.CreatedAt != "2025-10-28" {
		t.Errorf("expected earliest date 2025-10-28, got %s", common.CreatedAt)
	}
	if len(common.Sources) != 2 || common.Sources[0] != "javdb" || common.Sources[1] != "javbus" {
		t.Errorf("expected sources [javdb, javbus], got %v", common.Sources)
	}

	// Verify ranking: Subtitle first, then HD, then Size.
	// 1. common has Subtitle AND HD -> Rank 1
	// 2. hashJavDBOnly has Subtitle but NOT HD -> Rank 2
	// 3. hashJavBusOnly has HD but NOT Subtitle -> Rank 3
	if results[0].Hash != hashCommon {
		t.Errorf("expected rank 1 to be common, got %s", results[0].Hash)
	}
	if results[1].Hash != hashJavDBOnly {
		t.Errorf("expected rank 2 to be javdb-only (has subtitle), got %s", results[1].Hash)
	}
	if results[2].Hash != hashJavBusOnly {
		t.Errorf("expected rank 3 to be javbus-only (no subtitle), got %s", results[2].Hash)
	}
}

func TestAggregator_SingleSourceFailure(t *testing.T) {
	src1 := &stubSource{
		name:    "javdb",
		magnets: []domain.Magnet{{Hash: "1111111111111111111111111111111111111111", Name: "Item 1"}},
	}
	src2 := &stubSource{
		name: "javbus",
		err:  errors.New("network timeout"),
	}

	agg := NewAggregator([]Source{src1, src2}, time.Second, nil)
	results, err := agg.Find(t.Context(), domain.MovieRef{Code: "SSIS-001"})
	if err != nil {
		t.Fatalf("expected success when at least one source succeeds, got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 magnet, got %d", len(results))
	}
}

func TestAggregator_AllSourcesFailed(t *testing.T) {
	src1 := &stubSource{
		name: "javdb",
		err:  errors.New("javdb down"),
	}
	src2 := &stubSource{
		name: "javbus",
		err:  errors.New("javbus blocked"),
	}

	agg := NewAggregator([]Source{src1, src2}, time.Second, nil)
	_, err := agg.Find(t.Context(), domain.MovieRef{Code: "SSIS-001"})
	if err == nil {
		t.Fatalf("expected error when all sources fail")
	}
	if !domain.IsKind(err, domain.KindUpstream) {
		t.Errorf("expected KindUpstream error, got %v", err)
	}
}
