package magnet

import (
	"slices"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

func TestCleanTitle(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"【最新APP下载】SSIS-001", "SSIS-001"},
		{"[夸克网盘发布] ABP-123", "ABP-123"},
		{"【防失联地址】MIDE-456 完整版", "MIDE-456 完整版"},
		{"NORMAL-001", "NORMAL-001"},
	}

	for _, c := range cases {
		got := CleanTitle(c.input)
		if got != c.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestInfer(t *testing.T) {
	cases := []struct {
		name       string
		sub        bool
		fourK      bool
		uncensored bool
		cracked    bool
	}{
		{"SSIS-001_FHD_CH", true, false, false, false},
		{"SSIS-001-UC", true, false, true, false},
		{"SSIS-001-C 中文字幕", true, false, false, false},
		{"SSIS-001 4K UHD", false, true, false, false},
		{"SSIS-001 2160p", false, true, false, false},
		{"SSIS-001_uncensored", false, false, true, false},
		{"SSIS-001 流出破解版", false, false, false, true},
		{"(無修正-流出) SSIS-001 (Uncensored Leaked)一ヶ月間の禁欲の果てに", false, false, true, true},
		{"SSIS-001 禁欲生活 葵司 乙白", true, false, false, false}, // Chinese Hanzi title without Japanese kana
	}

	for _, c := range cases {
		inf := Infer(c.name)
		if inf.HasSubtitle != c.sub {
			t.Errorf("%s: subtitle = %v, want %v", c.name, inf.HasSubtitle, c.sub)
		}
		if inf.Has4K != c.fourK {
			t.Errorf("%s: 4K = %v, want %v", c.name, inf.Has4K, c.fourK)
		}
		if inf.HasUncensored != c.uncensored {
			t.Errorf("%s: uncensored = %v, want %v", c.name, inf.HasUncensored, c.uncensored)
		}
		if inf.HasCracked != c.cracked {
			t.Errorf("%s: cracked = %v, want %v", c.name, inf.HasCracked, c.cracked)
		}
	}
}

func TestApplyInference(t *testing.T) {
	m := domain.Magnet{
		Name: "SSIS-001 4K 破解版 中文字幕",
		Tags: []string{"高清"},
	}
	ApplyInference(&m)

	if !m.HasSubtitle {
		t.Errorf("expected HasSubtitle to be true")
	}
	if !m.Inferred {
		t.Errorf("expected Inferred to be true")
	}
	for _, expectedTag := range []string{"高清", "字幕", "4K", "破解"} {
		if !slices.Contains(m.Tags, expectedTag) {
			t.Errorf("expected tag %s in tags %v", expectedTag, m.Tags)
		}
	}
}
