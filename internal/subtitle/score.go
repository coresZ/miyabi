package subtitle

import (
	"sort"
	"strings"

	"github.com/ppxb/miyabi/internal/codeid"
)

// Candidate represents an available subtitle found either locally or from an online provider.
type Candidate struct {
	ID          string     `json:"id"`
	Provider    string     `json:"provider"`
	Name        string     `json:"name"`
	URL         string     `json:"url"`
	Ext         string     `json:"ext"`
	Language    Language   `json:"language"`
	Version     VersionTag `json:"version"`
	DisplayName string     `json:"display_name"`
	Score       int        `json:"score"`
	IsLocal     bool       `json:"is_local"`
}

// ScoreCandidate evaluates how well a candidate matches the given target catalogue code and video attributes.
func ScoreCandidate(c *Candidate, targetCode string, isUncensoredVideo bool) int {
	normTarget := codeid.Normalize(targetCode)
	cleanTarget := cleanCode(normTarget)

	nameLower := strings.ToLower(c.Name)
	cleanName := cleanCode(nameLower)

	score := 0

	// 1. Code match tier.
	if cleanTarget != "" && strings.Contains(cleanName, cleanTarget) {
		score += 800
	} else if normTarget != "" && strings.Contains(strings.ToUpper(c.Name), normTarget) {
		score += 600
	} else {
		tokens := strings.FieldsFunc(normTarget, func(r rune) bool {
			return r == '-' || r == '_' || r == '.' || r == ' '
		})
		for _, token := range tokens {
			if len(token) > 1 && strings.Contains(nameLower, strings.ToLower(token)) {
				score += 50
			}
		}
	}

	// 2. Language tier.
	switch c.Language {
	case LangSimplifiedChinese, LangTraditionalChinese:
		score += 150
	}

	// 3. Format tier.
	ext := strings.ToLower(c.Ext)
	if ext == "srt" || ext == "vtt" {
		score += 30
	} else if ext == "ass" || ext == "ssa" {
		score += 20
	}

	// 4. Version affinity tier.
	if isUncensoredVideo {
		if c.Version == VersionUncensored || c.Version == VersionLeaked {
			score += 150
		} else {
			score -= 50
		}
	} else {
		if c.Version == VersionStandard {
			score += 60
		} else if c.Version == VersionUncensored || c.Version == VersionLeaked {
			// Uncensored subtitles on standard/censored releases often have timing desync.
			score -= 80
		}
	}

	// 5. Local priority.
	if c.IsLocal {
		score += 200
	}

	c.Score = score
	if c.DisplayName == "" {
		c.DisplayName = BuildDisplayName(c.Language, c.Version, c.IsLocal)
	}
	return score
}

// RankCandidates scores and sorts candidates in descending order of relevance.
func RankCandidates(candidates []Candidate, targetCode string, isUncensoredVideo bool) []Candidate {
	scored := make([]Candidate, len(candidates))
	copy(scored, candidates)

	for i := range scored {
		ScoreCandidate(&scored[i], targetCode, isUncensoredVideo)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored
}

func cleanCode(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
