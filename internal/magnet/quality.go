package magnet

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/ppxb/miyabi/internal/domain"
)

var (
	// Ad banners often enclosed in brackets.
	adBracketRegex = regexp.MustCompile(`(?i)[【\[][^】\]]*(?:app|夸克|防失联|免费|地址|网址|发布|域名|代理|论坛|下载)[^】\]]*[】\]]`)

	// Subtitle patterns.
	fhdcRegex    = regexp.MustCompile(`(?i)(?:[^A-Za-z]|^)FHDC(?:[^A-Za-z]|$)`)
	subCodeRegex = regexp.MustCompile(`(?i)[-_](?:UC|CH?)(?:[^A-Za-z]|$)`)

	// 4K patterns.
	fourKRegex = regexp.MustCompile(`(?i)(?:[^A-Za-z0-9]|^)(?:4K(?:UHD)?|2160P)(?:[^A-Za-z0-9]|$)`)

	// Uncensored / Leaked patterns.
	uncensoredRegex       = regexp.MustCompile(`(?i)(?:[^A-Za-z0-9]|^)(?:uncensored|mosaic)(?:[^A-Za-z0-9]|$)`)
	uncensoredSuffixRegex = regexp.MustCompile(`(?i)[-_]U(?:C)?(?:[^A-Za-z0-9]|$)`)
	uncensoredWords       = []string{"无码", "無碼"}
	crackedWords          = []string{"破解", "破坏", "破壞", "流出"}

	versionModifierRegex = regexp.MustCompile(`(?i)(破解|破坏|破壞|无码|無碼|流出|高清|版本|原盘|壓制|压制|自制|精修|字幕)`)

	subtitleKeywords = []string{
		"中字", "中文", "字幕", "繁中", "简中", "汉化", "内嵌", "内封", "双语",
	}
)

type QualityInference struct {
	HasSubtitle   bool
	Has4K         bool
	HasUncensored bool
	HasCracked    bool
}

// CleanTitle strips common advertisement brackets from resource titles.
func CleanTitle(title string) string {
	cleaned := adBracketRegex.ReplaceAllString(title, "")
	return strings.TrimSpace(cleaned)
}

// Infer inspects the resource name and returns inferred quality attributes.
func Infer(name string) QualityInference {
	cleaned := CleanTitle(name)
	res := QualityInference{}

	// 1. Subtitle detection
	if fhdcRegex.MatchString(cleaned) || subCodeRegex.MatchString(cleaned) {
		res.HasSubtitle = true
	} else {
		for _, kw := range subtitleKeywords {
			if strings.Contains(cleaned, kw) {
				res.HasSubtitle = true
				break
			}
		}
		if !res.HasSubtitle && hasHanziWithoutKana(cleaned) {
			res.HasSubtitle = true
		}
	}

	// 2. 4K detection
	if fourKRegex.MatchString(cleaned) {
		res.Has4K = true
	}

	// 3. Uncensored / Cracked detection
	if uncensoredRegex.MatchString(cleaned) || uncensoredSuffixRegex.MatchString(cleaned) {
		res.HasUncensored = true
	} else {
		for _, w := range uncensoredWords {
			if strings.Contains(cleaned, w) {
				res.HasUncensored = true
				break
			}
		}
	}

	for _, w := range crackedWords {
		if strings.Contains(cleaned, w) {
			res.HasCracked = true
			break
		}
	}

	return res
}

// hasHanziWithoutKana checks if the text contains Chinese Hanzi characters
// (excluding generic version/release modifiers) and zero Japanese kana,
// which strongly indicates Chinese release translated naming.
func hasHanziWithoutKana(s string) bool {
	// Strip common release modifiers before checking for actual Chinese titles.
	stripped := versionModifierRegex.ReplaceAllString(s, "")
	hanCount := 0
	for _, r := range stripped {
		if unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return false
		}
		if unicode.Is(unicode.Han, r) {
			hanCount++
		}
	}
	// At least 2 content Hanzi characters without Japanese kana indicates a Chinese title.
	return hanCount >= 2
}

// ApplyInference enriches a domain.Magnet with inferred tags and attributes.
func ApplyInference(m *domain.Magnet) {
	inf := Infer(m.Name)
	newInferred := false

	existingTags := make(map[string]bool)
	for _, t := range m.Tags {
		existingTags[t] = true
	}

	addTag := func(tag string) {
		if !existingTags[tag] {
			existingTags[tag] = true
			m.Tags = append(m.Tags, tag)
			newInferred = true
		}
	}

	if inf.HasSubtitle {
		if !m.HasSubtitle {
			m.HasSubtitle = true
			newInferred = true
		}
		addTag("字幕")
	}

	if inf.Has4K {
		addTag("4K")
	}

	if inf.HasUncensored {
		addTag("无码")
	}

	if inf.HasCracked {
		addTag("破解")
	}

	if newInferred {
		m.Inferred = true
	}
}
