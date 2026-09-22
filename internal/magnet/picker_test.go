package magnet

import (
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

func TestPickerEmpty(t *testing.T) {
	p := NewPicker(DefaultPreferences())
	_, ok := p.Pick(nil)
	if ok {
		t.Fatal("expected pick on nil to return false")
	}
	_, ok = p.Pick([]domain.Magnet{})
	if ok {
		t.Fatal("expected pick on empty slice to return false")
	}
}

func TestPickerSubtitleRequired(t *testing.T) {
	p := NewPicker(Preferences{
		Subtitle:   PreferenceRequired,
		HD:         PreferenceAny,
		Uncensored: UncensoredAny,
	})

	magnets := []domain.Magnet{
		{Hash: "h1", Name: "SSIS-001 No Sub", HasSubtitle: false, Size: 5000},
		{Hash: "h2", Name: "SSIS-001 With Sub", HasSubtitle: true, Tags: []string{"字幕"}, Size: 2000},
	}

	best, ok := p.Pick(magnets)
	if !ok {
		t.Fatal("expected to find a matching magnet")
	}
	if best.Hash != "h2" {
		t.Fatalf("expected h2 (with sub), got %s", best.Hash)
	}

	// When all lack subtitle
	_, ok = p.Pick([]domain.Magnet{magnets[0]})
	if ok {
		t.Fatal("expected no match when subtitle is required and missing")
	}
}

func TestPickerHDRequired(t *testing.T) {
	p := NewPicker(Preferences{
		Subtitle:   PreferenceAny,
		HD:         PreferenceRequired,
		Uncensored: UncensoredAny,
	})

	magnets := []domain.Magnet{
		{Hash: "h1", Name: "SSIS-001 SD", HD: false, Size: 5000},
		{Hash: "h2", Name: "SSIS-001 HD", HD: true, Tags: []string{"高清"}, Size: 3000},
	}

	best, ok := p.Pick(magnets)
	if !ok {
		t.Fatal("expected to find a matching magnet")
	}
	if best.Hash != "h2" {
		t.Fatalf("expected h2 (HD), got %s", best.Hash)
	}

	_, ok = p.Pick([]domain.Magnet{magnets[0]})
	if ok {
		t.Fatal("expected no match when HD is required and missing")
	}
}

func TestPickerUncensoredExclude(t *testing.T) {
	p := NewPicker(Preferences{
		Subtitle:   PreferenceAny,
		HD:         PreferenceAny,
		Uncensored: UncensoredExclude,
	})

	magnets := []domain.Magnet{
		{Hash: "h1", Name: "SSIS-001-UC 无码破解", Tags: []string{"无码", "破解"}, Size: 5000},
		{Hash: "h2", Name: "SSIS-001.Leaked.720p", Size: 4000},
		{Hash: "h3", Name: "SSIS-001 Standard", Size: 3000},
	}

	best, ok := p.Pick(magnets)
	if !ok {
		t.Fatal("expected to find a matching magnet")
	}
	if best.Hash != "h3" {
		t.Fatalf("expected h3 (standard), got %s", best.Hash)
	}

	// When all are uncensored
	_, ok = p.Pick(magnets[:2])
	if ok {
		t.Fatal("expected no match when all are uncensored and uncensored is excluded")
	}
}

func TestPickerUncensoredRequired(t *testing.T) {
	p := NewPicker(Preferences{
		Subtitle:   PreferenceAny,
		HD:         PreferenceAny,
		Uncensored: UncensoredRequired,
	})

	magnets := []domain.Magnet{
		{Hash: "h1", Name: "SSIS-001 Standard", Size: 5000},
		{Hash: "h2", Name: "SSIS-001-UC 破解版", Size: 3000},
	}

	best, ok := p.Pick(magnets)
	if !ok {
		t.Fatal("expected to find a matching magnet")
	}
	if best.Hash != "h2" {
		t.Fatalf("expected h2 (uncensored), got %s", best.Hash)
	}
}

func TestPickerMaxSizeGiB(t *testing.T) {
	p := NewPicker(Preferences{
		Subtitle:   PreferenceAny,
		HD:         PreferenceAny,
		Uncensored: UncensoredAny,
		MaxSizeGiB: 5, // 5 GiB limit
	})

	magnets := []domain.Magnet{
		{Hash: "h1", Name: "SSIS-001 8GB", Size: 8 * 1024 * 1024 * 1024},
		{Hash: "h2", Name: "SSIS-001 4GB", Size: 4 * 1024 * 1024 * 1024},
	}

	best, ok := p.Pick(magnets)
	if !ok {
		t.Fatal("expected to find a matching magnet")
	}
	if best.Hash != "h2" {
		t.Fatalf("expected h2 (within size limit), got %s", best.Hash)
	}
}

func TestPickerScoringTiersAndInferredWeights(t *testing.T) {
	p := NewPicker(Preferences{
		Subtitle:   PreferencePreferred,
		HD:         PreferencePreferred,
		Uncensored: UncensoredPreferred,
	})

	// Verified Subtitle (10000) beats Inferred Subtitle (5000)
	verifiedSub := domain.Magnet{
		Hash:        "verified_sub",
		Name:        "SSIS-001",
		HasSubtitle: true,
		Tags:        []string{"字幕"},
		Size:        1000,
	}
	inferredSub := domain.Magnet{
		Hash:        "inferred_sub",
		Name:        "SSIS-001 中文字幕版", // title contains 中文字幕 -> inferred
		HasSubtitle: false,
		Inferred:    true,
		Size:        2000,
	}

	best, ok := p.Pick([]domain.Magnet{inferredSub, verifiedSub})
	if !ok {
		t.Fatal("expected match")
	}
	if best.Hash != "verified_sub" {
		t.Fatalf("expected verified subtitle to rank higher than inferred, got %s", best.Hash)
	}

	// Subtitle (10000) beats HD (1000)
	subOnly := domain.Magnet{
		Hash:        "sub_only",
		Name:        "SSIS-001",
		HasSubtitle: true,
		Tags:        []string{"字幕"},
		HD:          false,
		Size:        1000,
	}
	hdOnly := domain.Magnet{
		Hash:        "hd_only",
		Name:        "SSIS-001",
		HasSubtitle: false,
		HD:          true,
		Tags:        []string{"高清"},
		Size:        5000,
	}

	best, ok = p.Pick([]domain.Magnet{hdOnly, subOnly})
	if !ok {
		t.Fatal("expected match")
	}
	if best.Hash != "sub_only" {
		t.Fatalf("expected subtitle priority over HD, got %s", best.Hash)
	}

	// HD (1000) beats Uncensored (100)
	uncenOnly := domain.Magnet{
		Hash: "uncen_only",
		Name: "SSIS-001 无码流出",
		Size: 5000,
	}

	best, ok = p.Pick([]domain.Magnet{uncenOnly, hdOnly})
	if !ok {
		t.Fatal("expected match")
	}
	if best.Hash != "hd_only" {
		t.Fatalf("expected HD priority over Uncensored, got %s", best.Hash)
	}
}

func TestPickerTieBreaking(t *testing.T) {
	p := NewPicker(DefaultPreferences()) // preferred sub, preferred hd

	// Same score: larger size wins
	smaller := domain.Magnet{Hash: "h1", Name: "SSIS-001", HasSubtitle: true, HD: true, Size: 1000}
	larger := domain.Magnet{Hash: "h2", Name: "SSIS-001", HasSubtitle: true, HD: true, Size: 2000}

	best, _ := p.Pick([]domain.Magnet{smaller, larger})
	if best.Hash != "h2" {
		t.Fatalf("expected larger size h2 to win tie-break, got %s", best.Hash)
	}

	// Same score and size: more files wins
	fewerFiles := domain.Magnet{Hash: "f1", Name: "SSIS-001", HasSubtitle: true, HD: true, Size: 1000, FilesCount: 1}
	moreFiles := domain.Magnet{Hash: "f2", Name: "SSIS-001", HasSubtitle: true, HD: true, Size: 1000, FilesCount: 5}

	best, _ = p.Pick([]domain.Magnet{fewerFiles, moreFiles})
	if best.Hash != "f2" {
		t.Fatalf("expected more files f2 to win tie-break, got %s", best.Hash)
	}

	// Same score, size, and files: JavDB source wins
	javbusOnly := domain.Magnet{Hash: "b1", Name: "SSIS-001", HasSubtitle: true, HD: true, Size: 1000, FilesCount: 1, Sources: []string{"javbus"}}
	javdbSource := domain.Magnet{Hash: "d1", Name: "SSIS-001", HasSubtitle: true, HD: true, Size: 1000, FilesCount: 1, Sources: []string{"javdb"}}

	best, _ = p.Pick([]domain.Magnet{javbusOnly, javdbSource})
	if best.Hash != "d1" {
		t.Fatalf("expected JavDB source d1 to win tie-break, got %s", best.Hash)
	}
}
