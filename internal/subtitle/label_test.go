package subtitle

import (
	"testing"
)

func TestBuildDisplayName(t *testing.T) {
	tests := []struct {
		name    string
		lang    Language
		ver     VersionTag
		isLocal bool
		want    string
	}{
		{
			name: "simplified standard",
			lang: LangSimplifiedChinese,
			ver:  VersionStandard,
			want: "简体中文",
		},
		{
			name: "traditional standard",
			lang: LangTraditionalChinese,
			ver:  VersionStandard,
			want: "繁体中文",
		},
		{
			name: "simplified uncensored",
			lang: LangSimplifiedChinese,
			ver:  VersionUncensored,
			want: "简体中文（无码版）",
		},
		{
			name: "traditional uncensored",
			lang: LangTraditionalChinese,
			ver:  VersionUncensored,
			want: "繁体中文（无码版）",
		},
		{
			name: "simplified extended",
			lang: LangSimplifiedChinese,
			ver:  VersionExtended,
			want: "简体中文（加长版）",
		},
		{
			name: "traditional extended",
			lang: LangTraditionalChinese,
			ver:  VersionExtended,
			want: "繁体中文（加长版）",
		},
		{
			name: "simplified leaked",
			lang: LangSimplifiedChinese,
			ver:  VersionLeaked,
			want: "简体中文（流出版）",
		},
		{
			name:    "simplified local standard",
			lang:    LangSimplifiedChinese,
			ver:     VersionStandard,
			isLocal: true,
			want:    "简体中文（本地）",
		},
		{
			name:    "simplified local uncensored retains uncensored tag",
			lang:    LangSimplifiedChinese,
			ver:     VersionUncensored,
			isLocal: true,
			want:    "简体中文（无码版）",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildDisplayName(tt.lang, tt.ver, tt.isLocal)
			if got != tt.want {
				t.Errorf("BuildDisplayName(%q, %q, %v) = %q; want %q", tt.lang, tt.ver, tt.isLocal, got, tt.want)
			}
		})
	}
}

func TestDetectVersion(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     VersionTag
	}{
		{name: "standard", filename: "ABP-001.chs.srt", want: VersionStandard},
		{name: "uncensored english", filename: "ABP-001.uncensored.srt", want: VersionUncensored},
		{name: "uncensored chinese", filename: "ABP-001.无码破解.srt", want: VersionUncensored},
		{name: "uncensored mosaic", filename: "ABP-001.去mosaic.srt", want: VersionUncensored},
		{name: "leaked", filename: "ABP-001.流出完整版.srt", want: VersionLeaked},
		{name: "extended", filename: "ABP-001.extended.cut.srt", want: VersionExtended},
		{name: "extended chinese", filename: "ABP-001.加长版.srt", want: VersionExtended},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectVersion(tt.filename)
			if got != tt.want {
				t.Errorf("DetectVersion(%q) = %q; want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestDetectChineseLanguage(t *testing.T) {
	tests := []struct {
		name   string
		sample string
		hint   string
		want   Language
	}{
		{
			name:   "hint zh-tw",
			sample: "",
			hint:   "zh-TW",
			want:   LangTraditionalChinese,
		},
		{
			name:   "hint chs",
			sample: "",
			hint:   "chs",
			want:   LangSimplifiedChinese,
		},
		{
			name:   "simplified text sample",
			sample: "这是一个关于开发的问题，这个开关还在这里。",
			hint:   "",
			want:   LangSimplifiedChinese,
		},
		{
			name:   "traditional text sample",
			sample: "這是一個關於開發的問題，這個開關還在這裡。",
			hint:   "",
			want:   LangTraditionalChinese,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectChineseLanguage(tt.sample, tt.hint)
			if got != tt.want {
				t.Errorf("DetectChineseLanguage(%q, %q) = %q; want %q", tt.sample, tt.hint, got, tt.want)
			}
		})
	}
}
