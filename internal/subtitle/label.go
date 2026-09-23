package subtitle

import (
	"regexp"
	"strings"
)

// Language represents the recognized spoken/written language of a subtitle.
type Language string

const (
	LangSimplifiedChinese  Language = "zh-CN"
	LangTraditionalChinese Language = "zh-TW"
	LangJapanese           Language = "ja"
	LangEnglish            Language = "en"
	LangUnknown            Language = "unknown"
)

// VersionTag identifies release/cut variations of a video or subtitle.
type VersionTag string

const (
	VersionStandard   VersionTag = "standard"
	VersionUncensored VersionTag = "uncensored"
	VersionExtended   VersionTag = "extended"
	VersionLeaked     VersionTag = "leaked"
)

var (
	uncensoredPattern = regexp.MustCompile(`(?i)(uncensored|无码|無碼|破解|流出|mosaic|步兵)`)
	leakedPattern     = regexp.MustCompile(`(?i)(流出|leaked)`)
	extendedPattern   = regexp.MustCompile(`(?i)(extended|加长|加長|完整版|完全版)`)

	// Frequency counters for discriminating Simplified vs Traditional Chinese.
	simplifiedMarkers  = []rune("为与个么开关东风头后发这边过还进时样气应实话说问题现经体万台农国门书车云")
	traditionalMarkers = []rune("為與個麼開關東風頭後發這邊過還進時樣氣應實話說問題現經體萬臺農國門書車雲")
)

// BuildDisplayName constructs a standardized human-readable track label
// according to product rules (e.g. "简体中文", "繁体中文（无码版）", "简体中文（加长版）").
func BuildDisplayName(lang Language, ver VersionTag, isLocal bool) string {
	base := LangDisplayName(lang)
	var suffix string

	switch ver {
	case VersionUncensored:
		suffix = "（无码版）"
	case VersionExtended:
		suffix = "（加长版）"
	case VersionLeaked:
		suffix = "（流出版）"
	}

	if isLocal && suffix == "" {
		suffix = "（本地）"
	}

	return base + suffix
}

// LangDisplayName returns the standard Chinese description of the language.
func LangDisplayName(lang Language) string {
	switch lang {
	case LangSimplifiedChinese:
		return "简体中文"
	case LangTraditionalChinese:
		return "繁体中文"
	case LangJapanese:
		return "日本语"
	case LangEnglish:
		return "英语"
	default:
		return "中文字幕"
	}
}

// DetectVersion inspects a filename or metadata title for cut/version markers.
func DetectVersion(name string) VersionTag {
	if leakedPattern.MatchString(name) {
		return VersionLeaked
	}
	if uncensoredPattern.MatchString(name) {
		return VersionUncensored
	}
	if extendedPattern.MatchString(name) {
		return VersionExtended
	}
	return VersionStandard
}

// IsUncensored reports whether a video or subtitle name indicates an uncensored or leaked release.
func IsUncensored(name string) bool {
	ver := DetectVersion(name)
	return ver == VersionUncensored || ver == VersionLeaked
}

// DetectChineseLanguage inspects sample text or hints to determine whether
// Chinese text is Simplified or Traditional.
func DetectChineseLanguage(sample string, hint string) Language {
	hintLower := strings.ToLower(hint)
	switch {
	case strings.Contains(hintLower, "zh-tw") || strings.Contains(hintLower, "cht") || strings.Contains(hintLower, "繁"):
		return LangTraditionalChinese
	case strings.Contains(hintLower, "zh-cn") || strings.Contains(hintLower, "chs") || strings.Contains(hintLower, "简"):
		return LangSimplifiedChinese
	}

	if sample == "" {
		return LangSimplifiedChinese
	}

	// Truncate to first 15000 characters to ensure fast inspection.
	runes := []rune(sample)
	if len(runes) > 15000 {
		runes = runes[:15000]
	}

	var simpCount, tradCount int
	for _, r := range runes {
		for _, sm := range simplifiedMarkers {
			if r == sm {
				simpCount++
				break
			}
		}
		for _, tm := range traditionalMarkers {
			if r == tm {
				tradCount++
				break
			}
		}
	}

	if tradCount > simpCount {
		return LangTraditionalChinese
	}
	if simpCount > 0 {
		return LangSimplifiedChinese
	}

	return LangSimplifiedChinese
}

// FormatVersionDescription returns a concise version label for UI badges.
func FormatVersionDescription(ver VersionTag) string {
	switch ver {
	case VersionUncensored:
		return "无码版"
	case VersionExtended:
		return "加长版"
	case VersionLeaked:
		return "流出版"
	case VersionStandard:
		return "标准版"
	default:
		return string(ver)
	}
}
