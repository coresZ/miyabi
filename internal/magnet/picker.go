package magnet

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
)

// PreferenceLevel specifies user tolerance for attributes like subtitles or HD.
type PreferenceLevel string

const (
	PreferencePreferred PreferenceLevel = "preferred" // 优先
	PreferenceRequired  PreferenceLevel = "required"  // 必须
	PreferenceAny       PreferenceLevel = "any"       // 不限
)

// UncensoredFilter specifies user tolerance or exclusion for uncensored / leaked releases.
type UncensoredFilter string

const (
	UncensoredPreferred UncensoredFilter = "preferred" // 优先
	UncensoredRequired  UncensoredFilter = "required"  // 必须
	UncensoredExclude   UncensoredFilter = "exclude"   // 排除
	UncensoredAny       UncensoredFilter = "any"       // 不限
)

// Preferences configures magnet selection criteria.
type Preferences struct {
	Subtitle   PreferenceLevel  `json:"subtitle"`
	HD         PreferenceLevel  `json:"hd"`
	Uncensored UncensoredFilter `json:"uncensored"`
	MaxSizeGiB int64            `json:"max_size_gib"` // 0 indicates no limit
}

// DefaultPreferences returns the default magnet selection preferences.
func DefaultPreferences() Preferences {
	return Preferences{
		Subtitle:   PreferencePreferred,
		HD:         PreferencePreferred,
		Uncensored: UncensoredAny,
		MaxSizeGiB: 0,
	}
}

// Picker selects the best matching magnet from candidates according to user preferences.
type Picker struct {
	prefs Preferences
}

// NewPicker creates a magnet picker with the provided preferences.
func NewPicker(prefs Preferences) *Picker {
	normalized := prefs
	if normalized.Subtitle == "" {
		normalized.Subtitle = PreferencePreferred
	}
	if normalized.HD == "" {
		normalized.HD = PreferencePreferred
	}
	if normalized.Uncensored == "" {
		normalized.Uncensored = UncensoredAny
	}
	return &Picker{prefs: normalized}
}

type candidate struct {
	magnet   domain.Magnet
	score    int
	hasSub   bool
	subInf   bool
	hasHD    bool
	hdInf    bool
	hasUncen bool
	uncenInf bool
}

// Pick filters and scores candidate magnets according to preferences,
// returning the highest scoring magnet or (zero, false) if no qualified magnet exists.
func (p *Picker) Pick(magnets []domain.Magnet) (domain.Magnet, bool) {
	if len(magnets) == 0 {
		return domain.Magnet{}, false
	}

	var passed []candidate
	maxSizeBytes := p.prefs.MaxSizeGiB * 1024 * 1024 * 1024

	for _, m := range magnets {
		// Ensure tags and inference are applied
		magnetCopy := m
		ApplyInference(&magnetCopy)

		hasSub, subInf := inspectSubtitle(magnetCopy)
		hasHD, hdInf := inspectHD(magnetCopy)
		hasUncen, uncenInf := inspectUncensored(magnetCopy)

		// 1. Filtering phase
		if p.prefs.Subtitle == PreferenceRequired && !hasSub {
			continue
		}
		if p.prefs.HD == PreferenceRequired && !hasHD {
			continue
		}
		if p.prefs.Uncensored == UncensoredExclude && hasUncen {
			continue
		}
		if p.prefs.Uncensored == UncensoredRequired && !hasUncen {
			continue
		}
		if maxSizeBytes > 0 && magnetCopy.Size > maxSizeBytes {
			continue
		}

		// 2. Scoring phase
		// Score tiers: Subtitle (10000) > HD (1000) > Uncensored (100)
		// Inferred tags receive half weight.
		score := 0

		if p.prefs.Subtitle == PreferencePreferred && hasSub {
			if subInf {
				score += 5000
			} else {
				score += 10000
			}
		}

		if p.prefs.HD == PreferencePreferred && hasHD {
			if hdInf {
				score += 500
			} else {
				score += 1000
			}
		}

		if p.prefs.Uncensored == UncensoredPreferred && hasUncen {
			if uncenInf {
				score += 50
			} else {
				score += 100
			}
		}

		passed = append(passed, candidate{
			magnet:   magnetCopy,
			score:    score,
			hasSub:   hasSub,
			subInf:   subInf,
			hasHD:    hasHD,
			hdInf:    hdInf,
			hasUncen: hasUncen,
			uncenInf: uncenInf,
		})
	}

	if len(passed) == 0 {
		return domain.Magnet{}, false
	}

	// Tie-breaking: score > size > files_count > JavDB source > CreatedAt
	slices.SortStableFunc(passed, func(a, b candidate) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		if a.magnet.Size != b.magnet.Size {
			if a.magnet.Size > b.magnet.Size {
				return -1
			}
			return 1
		}
		if a.magnet.FilesCount != b.magnet.FilesCount {
			if a.magnet.FilesCount > b.magnet.FilesCount {
				return -1
			}
			return 1
		}
		aJavDB := slices.Contains(a.magnet.Sources, "javdb")
		bJavDB := slices.Contains(b.magnet.Sources, "javdb")
		if aJavDB != bJavDB {
			if aJavDB {
				return -1
			}
			return 1
		}
		if a.magnet.CreatedAt != "" && b.magnet.CreatedAt != "" && a.magnet.CreatedAt != b.magnet.CreatedAt {
			return cmp.Compare(a.magnet.CreatedAt, b.magnet.CreatedAt)
		}
		return 0
	})

	return passed[0].magnet, true
}

func inspectSubtitle(m domain.Magnet) (has bool, inferred bool) {
	if m.HasSubtitle || slices.Contains(m.Tags, "字幕") {
		// If inferred is true, check whether subtitle was from inference
		inf := Infer(m.Name)
		if m.Inferred && inf.HasSubtitle && !slices.Contains(m.Sources, "javdb") && !slices.Contains(m.Sources, "javbus") {
			return true, true
		}
		// If magnet has inferred flag and source didn't explicitly tag it
		if m.Inferred && !hasExplicitSubtitleTag(m) {
			return true, true
		}
		return true, false
	}
	inf := Infer(m.Name)
	if inf.HasSubtitle {
		return true, true
	}
	return false, false
}

func hasExplicitSubtitleTag(m domain.Magnet) bool {
	// If m was from javdb or javbus and had HasSubtitle true, it was explicit
	return m.HasSubtitle && (!m.Inferred || !Infer(m.Name).HasSubtitle)
}

func inspectHD(m domain.Magnet) (has bool, inferred bool) {
	if m.HD || slices.Contains(m.Tags, "高清") {
		return true, false
	}
	if slices.Contains(m.Tags, "4K") || Infer(m.Name).Has4K {
		return true, true
	}
	return false, false
}

func inspectUncensored(m domain.Magnet) (has bool, inferred bool) {
	if slices.Contains(m.Tags, "无码") || slices.Contains(m.Tags, "破解") {
		return true, true
	}
	inf := Infer(m.Name)
	if inf.HasUncensored || inf.HasCracked {
		return true, true
	}
	nameLower := strings.ToLower(m.Name)
	if strings.Contains(nameLower, "leaked") || strings.Contains(nameLower, "uncensored") {
		return true, true
	}
	return false, false
}
